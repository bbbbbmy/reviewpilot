# Operations

Day-to-day operational guide: backups, upgrades, log/metric
interpretation, troubleshooting.

## Backup

The entire state of ReviewPilot is a single SQLite file at
`RP_DB_PATH` (default `/var/lib/reviewpilot/review.db`). SQLite's WAL
mode means concurrent reads are safe; copying the file while the server
runs is safe.

### Docker Compose

```bash
docker compose exec reviewpilot sqlite3 /var/lib/reviewpilot/review.db \
    ".backup '/var/lib/reviewpilot/review.backup.db'"
docker compose cp reviewpilot:/var/lib/reviewpilot/review.backup.db \
    ./review-$(date +%F).db
```

Schedule daily with cron + `aws s3 cp` / `restic backup` / similar.

### Kubernetes

```bash
kubectl exec -n reviewpilot deploy/reviewpilot -- \
    sqlite3 /var/lib/reviewpilot/review.db \
    ".backup '/var/lib/reviewpilot/review.backup.db'"
kubectl cp reviewpilot/$(kubectl get pod -n reviewpilot -l app=reviewpilot -o name | head -1 | cut -d/ -f2):/var/lib/reviewpilot/review.backup.db \
    ./review-$(date +%F).db
```

Back up the PVC (Velero / snapshots) is also viable.

### Restore

```bash
docker compose stop reviewpilot
docker compose cp ./review-2026-08-22.db reviewpilot:/var/lib/reviewpilot/review.db
docker compose start reviewpilot
```

The server picks up the restored file on next startup.

## Upgrade

```bash
cd /srv/reviewpilot
docker compose pull
docker compose up -d
docker compose logs -f reviewpilot  # confirm /healthz
```

Watch for these logs at startup:

```
{"level":"INFO","msg":"reviewpilot server starting","version":"v0.5.0"}
```

The version string is baked into the binary at build time
(`-ldflags "-X main.version=v0.5.0"`). If you see the old version
after `docker compose pull`, you forgot to rebuild the image
(`docker compose build` before `up -d`).

Schema migrations (`internal/webdashboard/migrations/*.sql`) apply on
startup. The `webdashboard.Open()` function reads the embedded SQL
files in lexical order and executes them — SQLite's `IF NOT EXISTS`
clauses make this idempotent.

## Observability

### Logs

JSON to stderr. One line per event. Useful queries:

```bash
# All webhook signature failures in the last hour.
docker compose logs --since=1h reviewpilot | jq 'select(.msg | test("signature"))'

# All LLM calls by token usage.
docker compose logs --since=24h reviewpilot | jq 'select(.msg | test("llm chat")) | .tokens_in, .tokens_out'
```

### Metrics

`GET /metrics` (Prometheus exposition). Counters are monotonically
increasing; histograms give p50/p95/p99 latency.

| Metric | Type | Use |
|---|---|---|
| `reviewpilot_review_total{status="done"}` | counter | Total reviews completed |
| `reviewpilot_review_total{status="failed"}` | counter | Reviews where the LLM or pipeline hard-failed |
| `reviewpilot_review_total{status="skipped"}` | counter | Reviews skipped due to size / missing diff |
| `reviewpilot_review_tokens_total{stage="in"}` | counter | Cumulative input tokens (LLM cost driver) |
| `reviewpilot_review_tokens_total{stage="out"}` | counter | Cumulative output tokens |
| `reviewpilot_review_step_duration_seconds{step="split"}` | histogram | Diff splitting time |
| `reviewpilot_review_step_duration_seconds{step="context"}` | histogram | ChatRequest assembly time |
| `reviewpilot_review_step_duration_seconds{step="llm"}` | histogram | LLM round-trip time |
| `reviewpilot_review_step_duration_seconds{step="dedup"}` | histogram | Dedup time |

Recommended alerts (Prometheus):

```yaml
groups:
  - name: reviewpilot
    rules:
      - alert: ReviewPilotWebhookFailing
        expr: |
          rate(reviewpilot_review_total{status="failed"}[10m]) > 0.1
        for: 10m
      - alert: ReviewPilotDown
        expr: up{job="reviewpilot"} == 0
        for: 2m
      - alert: ReviewPilotLLMSlow
        expr: |
          histogram_quantile(0.95, rate(reviewpilot_review_step_duration_seconds_bucket{step="llm"}[5m])) > 30
        for: 10m
      - alert: ReviewPilotHighFailureRate
        expr: |
          rate(reviewpilot_review_total{status="failed"}[1h])
            / rate(reviewpilot_review_total[1h]) > 0.05
        for: 30m
```

## Troubleshooting

### Symptom: server exits immediately after start

**Cause**: missing required env var.

**Diagnosis**:

```bash
docker compose logs reviewpilot 2>&1 | grep -E "missing|required"
```

Most common: `RP_ADMIN_USER` and `RP_ADMIN_PASS` not set. ReviewPilot
refuses to start without them (production safety). Set
`RP_DEV_MODE=1` to bypass for local development.

### Symptom: webhook returns 503

**Cause**: missing `RP_GITHUB_WEBHOOK_SECRET` or `RP_GITHUB_TOKEN`.

**Diagnosis**:

```bash
curl -X POST http://localhost:8080/webhook -d '{}' -H 'Content-Type: application/json'
# {"message":"webhook disabled: platformgithub: required environment variable is missing: RP_GITHUB_TOKEN"}
```

Set the missing variable and restart.

### Symptom: webhook returns 401

**Cause**: HMAC signature mismatch.

**Diagnosis**:

```bash
docker compose logs --tail=50 reviewpilot | grep "webhook signature verify"
# {"level":"WARN","msg":"webhook signature verify failed","err":"platformgithub: signature mismatch"}
```

Three sub-causes:

1. GitHub's webhook secret doesn't match `RP_GITHUB_WEBHOOK_SECRET`
   (typo, env var never propagated, secret rotated on one side).
2. A proxy is rewriting the body (nginx buffering / chunked encoding).
   `client_max_body_size 25m` + `proxy_buffering off` if using nginx.
3. The body was JSON-decoded before HMAC was verified (NOT what
   ReviewPilot does — but worth checking if you've customized).

### Symptom: reviews return `skipped` with `chunk too large`

**Cause**: single file in the diff exceeds `max_input_tokens_per_pr`.

**Diagnosis**: dashboard → repo → raise the per-repo limit; or wait for
hunk-level splitting (planned for v0.5.0).

### Symptom: high LLM cost

**Diagnosis**: dashboard → metrics → `rate(reviewpilot_review_tokens_total[1d])`.

Mitigations:

- Per-repo `max_input_tokens_per_pr` ceiling (lower = fewer tokens).
- Per-repo file whitelist (only review changed files in
  `internal/**/*.go`, not `*.lock`).
- Switch to a smaller / cheaper model (e.g., `gpt-4o-mini` is ~30×
  cheaper than `gpt-4o` for most reviews).
- (Planned v0.5.0) Per-PR comment cap: review-pilot currently posts
  every comment the LLM produces; a cap would prevent runaway.

### Symptom: SQLite locked

**Cause**: two processes pointed at the same DB file (e.g., two
`reviewpilot` containers sharing a volume).

**Diagnosis**: `docker compose ps` — if you see more than one
`reviewpilot` container, that's the bug.

**Fix**: scale ReviewPilot horizontally by running a second instance
with a **separate** SQLite file, not by sharing one volume.

### Symptom: dashboard renders but is empty

**Cause**: SQLite was reset (e.g., a backup restore) and the engine
hasn't run any reviews yet. The dashboard reads from `webdashboard`
store, which is populated by the webhook handler on each review.

**Fix**: trigger a test webhook or wait for the next PR.

## Recovery procedures

### Reset database (lose all history)

```bash
docker compose stop reviewpilot
docker compose run --rm reviewpilot \
    sh -c "rm -f /var/lib/reviewpilot/review.db /var/lib/reviewpilot/review.db-*"
docker compose up -d
```

The empty schema is re-created on next startup.

### Rotate `RP_ADMIN_PASS`

1. Update `RP_ADMIN_PASS` on the server.
2. Restart.
3. Update your password manager / browser.
4. Old sessions are invalidated immediately (Basic Auth is stateless).

### Rotate `RP_GITHUB_TOKEN`

1. Generate a new fine-grained PAT.
2. Update `RP_GITHUB_TOKEN`.
3. Restart.
4. Old token can be revoked at GitHub after the server has picked up
   the new one (check logs for successful outbound calls).

### Rotate `RP_LLM_OPENAI_API_KEY`

Same as `RP_GITHUB_TOKEN`. The server doesn't cache the key, so a
restart is enough.