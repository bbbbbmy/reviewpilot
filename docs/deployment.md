# Deployment

This guide covers production-grade deployment of ReviewPilot.
For local development, see the [README](../README.md#quick-start-docker-compose).

## Prerequisites

- Linux host (amd64 or arm64) with Docker 24+
- A GitHub account (or GitHub Enterprise instance)
- An OpenAI-compatible LLM API key
- DNS record pointing at your host (e.g., `reviewpilot.example.com`)
- 2 vCPU, 1 GB RAM, 5 GB disk minimum

## 1. TLS termination

ReviewPilot itself speaks plaintext HTTP. Run it behind a reverse
proxy that terminates TLS and forwards to the upstream.

### Caddy (simplest)

`/etc/caddy/Caddyfile`:

```
reviewpilot.example.com {
    basicauth {
        admin {$ADMIN_PASS_HASH}
    }
    reverse_proxy 127.0.0.1:8080
}
```

But this exposes the dashboard's Basic Auth to the world, which is what
you want for a self-hosted tool. The downside: **anyone on the internet
can attempt login**. Mitigate with:

- A long, random `RP_ADMIN_PASS` (≥32 chars)
- Rate limiting at Caddy (`ratelimit` directive)
- IP allowlist at the firewall

Better: **don't expose the dashboard directly**. Put it on a private
network and only expose `POST /webhook` to GitHub:

```
reviewpilot.example.com {
    reverse_proxy 127.0.0.1:8080
}
127.0.0.1:8080 {
    # bind only on localhost; expose via SSH tunnel / VPN
}
```

Then GitHub sends webhooks to `https://reviewpilot.example.com/webhook`,
but you reach the dashboard only via VPN.

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name reviewpilot.example.com;

    ssl_certificate     /etc/letsencrypt/live/reviewpilot.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/reviewpilot.example.com/privkey.pem;

    # /webhook is the only path GitHub needs.
    location = /webhook {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        client_max_body_size 25m;
    }

    # Everything else: 404 (or put behind VPN/SSH tunnel).
    location / {
        return 404;
    }
}
```

Note `client_max_body_size 25m` — GitHub's `pull_request.patch` field
can be up to ~20 MiB for huge diffs.

## 2. Docker Compose (production)

`/srv/reviewpilot/docker-compose.yml`:

```yaml
services:
  reviewpilot:
    build:
      context: .
      args:
        VERSION: v0.4.0
    image: bbbbbmy/reviewpilot:v0.4.0
    container_name: reviewpilot
    restart: unless-stopped
    ports:
      # Bind only on localhost — let nginx / caddy forward in.
      - "127.0.0.1:8080:8080"
    environment:
      RP_HTTP_LISTEN: ":8080"
      RP_ADMIN_USER: ${admin
      RP_ADMIN_PASS: ${admin_pass
      RP_GITHUB_WEBHOOK_SECRET: ${github_webhook_secret
      RP_GITHUB_TOKEN: ${github_token
      RP_LLM_OPENAI_API_KEY: ${llm_api_key
      RP_LLM_OPENAI_MODEL: "gpt-4o"
      RP_DB_PATH: /var/lib/reviewpilot/review.db
    volumes:
      - reviewpilot-data:/var/lib/reviewpilot
    healthcheck:
      test: ["CMD", "/usr/local/bin/reviewpilot", "--healthcheck-only"]
      interval: 30s
      timeout: 5s
      retries: 3
    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

volumes:
  reviewpilot-data:
    driver: local
```

`/srv/reviewpilot/.env`:

```
ADMIN_USER=admin
ADMIN_PASS=$(openssl rand -base64 48)
GITHUB_WEBHOOK_SECRET=$(openssl rand -base64 48)
GITHUB_TOKEN=ghp_xxx
LLM_API_KEY=sk-xxx
```

```bash
cd /srv/reviewpilot
docker compose pull
docker compose up -d
docker compose logs -f reviewpilot
```

## 3. Kubernetes

Minimal manifest (Helm chart is planned, not yet shipped):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: reviewpilot
spec:
  replicas: 1   # SQLite is local; multiple replicas don't share state
  selector:
    matchLabels: { app: reviewpilot }
  template:
    metadata:
      labels: { app: reviewpilot }
    spec:
      containers:
        - name: reviewpilot
          image: bbbbbmy/reviewpilot:v0.4.0
          ports:
            - containerPort: 8080
          env:
            - name: RP_HTTP_LISTEN
              value: ":8080"
            - name: RP_ADMIN_USER
              valueFrom: { secretKeyRef: { name: reviewpilot, key: admin-user } }
            - name: RP_ADMIN_PASS
              valueFrom: { secretKeyRef: { name: reviewpilot, key: admin-pass } }
            - name: RP_GITHUB_WEBHOOK_SECRET
              valueFrom: { secretKeyRef: { name: reviewpilot, key: github-webhook-secret } }
            - name: RP_GITHUB_TOKEN
              valueFrom: { secretKeyRef: { name: reviewpilot, key: github-token } }
            - name: RP_LLM_OPENAI_API_KEY
              valueFrom: { secretKeyRef: { name: reviewpilot, key: llm-api-key } }
            - name: RP_DB_PATH
              value: /var/lib/reviewpilot/review.db
          volumeMounts:
            - name: data
              mountPath: /var/lib/reviewpilot
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
            initialDelaySeconds: 5
            periodSeconds: 30
          resources:
            requests: { cpu: "100m", memory: "128Mi" }
            limits:   { cpu: "500m", memory: "512Mi" }
      volumes:
        - name: data
          persistentVolumeClaim: { claimName: reviewpilot-data }
```

`StorageClass: encrypted` is strongly recommended.

## 4. Backup

The entire state of ReviewPilot is one SQLite file:

```bash
# Snapshot the volume while the server is running — SQLite WAL
# handles the lock-free copy cleanly.
docker compose exec reviewpilot sqlite3 /var/lib/reviewpilot/review.db ".backup '/var/lib/reviewpilot/review.backup.db'"
docker compose cp reviewpilot:/var/lib/reviewpilot/review.backup.db ./review-$(date +%F).db
```

Schedule this with cron + S3 / B2 / restic.

To restore: stop the server, replace `review.db`, restart.

## 5. Upgrade

```bash
cd /srv/reviewpilot
docker compose pull                  # fetches the new image
docker compose up -d                # recreates the container
docker compose logs -f reviewpilot  # confirm /healthz
```

Each release's migration history lives in
`openspec/changes/archive/2026-08-22-…` in the `vibecoding` repo.
ReviewPilot's own migrations are embedded SQL files (`internal/webdashboard/migrations/`).
The server applies them on startup; no manual `migrate` step is needed.

## 6. Monitoring

`GET /metrics` exposes Prometheus metrics:

| Metric | Type | Labels | What |
|---|---|---|---|
| `reviewpilot_review_total` | counter | `status` | Number of review runs by terminal status (`done` / `failed` / `skipped`) |
| `reviewpilot_review_tokens_total` | counter | `stage` (`in` / `out`) | LLM tokens consumed |
| `reviewpilot_review_step_duration_seconds` | histogram | `step` | Per-step pipeline duration |

Recommended alerts:

- `rate(reviewpilot_review_total{status="failed"}[1h]) > 3` — too many failed reviews
- `histogram_quantile(0.95, rate(reviewpilot_review_step_duration_seconds_bucket{step="llm"}[5m])) > 30` — LLM too slow
- `up{job="reviewpilot"} == 0` — server unreachable

Scrape config:

```yaml
scrape_configs:
  - job_name: reviewpilot
    metrics_path: /metrics
    static_configs:
      - targets: ['reviewpilot.internal:8080']
    basic_auth:
      username: admin
      password_file: /etc/prometheus/reviewpilot-admin-pass
```

Yes, `/metrics` is currently behind Basic Auth. If you'd rather
expose it without auth, file a follow-up — v0.4.0 mirrors the dashboard
choice for simplicity.

## 7. Health checks

`GET /healthz` returns 200 OK with `{"status":"ok"}` if the server is
running. It does **not** check DB connectivity in v0.4.0 — a follow-up
could add `SELECT 1` here. Use `docker compose ps` / Kubernetes
liveness probes for now.

## 8. Log shipping

The server emits one JSON line per event to stderr. Pipe to your
favourite aggregator:

```yaml
logging:
  driver: json-file
  options:
    max-size: "10m"
    max-file: "3"
```

Then `docker compose logs` or a Loki / Vector pipeline picks them up.

If you see persistent `webhook signature verify failed`, your
`RP_GITHUB_WEBHOOK_SECRET` doesn't match what GitHub has stored on the
webhook — fix one of them.

## 9. Troubleshooting

| Symptom | Likely cause |
|---|---|
| Server exits immediately | Missing `RP_ADMIN_USER` or `RP_ADMIN_PASS` |
| `/webhook` returns 503 | `RP_GITHUB_WEBHOOK_SECRET` / `RP_GITHUB_TOKEN` missing or wrong |
| `401 Unauthorized` from webhook | Signature mismatch — check GitHub's webhook secret matches `RP_GITHUB_WEBHOOK_SECRET` byte-for-byte |
| All reviews return `skipped` with `reason: chunk too large` | Single file exceeds `max_input_tokens_per_pr`. Raise the per-repo limit in the dashboard or split the PR. |
| LLM calls hang | Network egress to the LLM provider blocked. Test with `curl https://api.openai.com/v1/models -H "Authorization: Bearer $RP_LLM_OPENAI_API_KEY"` |
| Comments posted but no record in the UI | SQLite not writable. `docker compose exec reviewpilot ls -la /var/lib/reviewpilot/` |
| High LLM cost | Lower `max_input_tokens_per_pr` per repo; switch to a cheaper model for noisy PRs. |