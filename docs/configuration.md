# Configuration

Every ReviewPilot setting is driven by environment variables.
This document lists every variable, what it does, and sensible defaults.

## Server (HTTP + webhook)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `RP_HTTP_LISTEN` | no | `:8080` | Listen address. Use `:8443` behind a reverse proxy, or `unix:///var/run/reviewpilot.sock` if you're feeling creative. |
| `RP_ADMIN_USER` | **yes** | — | Basic Auth username for the dashboard + JSON API. |
| `RP_ADMIN_PASS` | **yes** | — | Basic Auth password. Use a random ≥32 char string. **Server refuses to start without these.** |
| `RP_DEV_MODE` | no | `0` | Set to `1` to skip Basic Auth (don't use in production). |
| `RP_DB_PATH` | no | `/var/lib/reviewpilot/review.db` | SQLite file path. |
| `RP_GITHUB_WEBHOOK_SECRET` | **yes (server)** | — | The secret you set when registering the GitHub webhook. The handler refuses any request whose `X-Hub-Signature-256` header doesn't match HMAC-SHA256(body, secret). |
| `RP_GITHUB_TOKEN` | **yes (server)** | — | GitHub API bearer token. Use a fine-grained PAT scoped to the repos you want to review, or a pre-minted GitHub App installation token. |
| `RP_GITHUB_APP_ID` | reserved | — | GitHub App ID. **Not yet consumed** — full JWT exchange is a follow-up. |
| `RP_GITHUB_APP_PRIVATE_KEY` | reserved | — | PEM-encoded RSA private key for the App. **Not yet consumed**. |
| `RP_GITHUB_BASE_URL` | no | `https://api.github.com` | Override for GitHub Enterprise. |

### GitHub webhook behaviour when creds are missing

`POST /webhook` returns `503 Service Unavailable` with a JSON error
explaining which credential is missing. The server keeps running and
`GET /healthz` / `GET /metrics` still work, so monitoring stays green
while you wire up secrets.

## LLM provider

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `RP_LLM_OPENAI_API_KEY` | **yes** | — | Provider API key. |
| `RP_LLM_OPENAI_BASE_URL` | no | `https://api.openai.com/v1` | OpenAI-compatible base URL. Set to `https://api.deepseek.com/v1`, `https://api.moonshot.cn/v1`, `https://openrouter.ai/api/v1`, etc. |
| `RP_LLM_OPENAI_MODEL` | no | `gpt-4o-mini` | Model name passed on every request. Pick anything the upstream supports. |

Any service implementing OpenAI's `/v1/chat/completions` wire format
works. `internal/llmopenai/openai.go` does the protocol-level work; no
SDK is used.

## CLI only (`reviewpilot review`)

CLI mode requires only `RP_LLM_OPENAI_API_KEY` (+ optional base URL +
model). It does **not** need GitHub or admin credentials.

## SQLite store

There is no separate config for SQLite beyond `RP_DB_PATH`. WAL mode is
enabled automatically; foreign keys too. To inspect the database
during debugging:

```bash
sqlite3 /var/lib/reviewpilot/review.db
> .schema review
> SELECT id, repo_id, pr_number, status, tokens_in FROM review ORDER BY started_at DESC LIMIT 5;
```

The schema lives at `internal/webdashboard/migrations/0001_init.sql`.

## Scrubbing

The server wraps its `slog.JSONHandler` with
`llmopenai.ScrubbingHandler` which redacts:

- Attribute keys: `authorization`, `api_key`, `api-key`, `x-api-key`, `bearer`
- String values containing substring `bearer ` (covers things like
  `Authorization: Bearer sk-xxx`)
- String values starting with `sk-`

If you ever need to log the request body that contains an auth header,
it will already be redacted. You can verify:

```bash
./bin/reviewpilot server 2>&1 | grep -E "Authorization|sk-"
# → only "<redacted>"
```

## What's intentionally NOT a configuration knob

- **SQLite busy timeout**: hardcoded (use retry loop in app code, not in
  DB layer — keeps queries predictable).
- **Log format**: `slog.JSONHandler` to stderr. If you want pretty logs,
  run behind `slog-dedup` or pipe through `lnav`.
- **HTTP timeouts**: hardcoded (`ReadHeaderTimeout=5s`,
  `Read/WriteTimeout=30s`, `IdleTimeout=60s`). Override via a reverse
  proxy if you need different values.
- **Worker pool size**: there is no worker pool. The server is single-process,
  single-threaded for HTTP. For ≥10 RPS concurrent reviews, run multiple
  instances behind a load balancer (each with its own SQLite file —
  sharing SQLite across processes works but is not recommended).

## Defaults that will probably need to change

- `RP_LLM_OPENAI_MODEL=gpt-4o-mini` is fine for smoke tests; production
  reviewers may want `gpt-4o` or a smaller model for cost. Set per-repo in
  the dashboard.
- `RP_DB_PATH=/var/lib/reviewpilot/review.db` assumes a Linux host with
  that directory writable. On macOS dev, try `~/Library/Application
  Support/reviewpilot/review.db`.
- `RP_HTTP_LISTEN=:8080` is fine for containerized deployments behind
  a reverse proxy. Bare-metal should use `:8443` with TLS termination at
  nginx / caddy / traefik.

## Security checklist

- [ ] `RP_ADMIN_PASS` is ≥32 random chars (`openssl rand -base64 48`)
- [ ] `RP_GITHUB_WEBHOOK_SECRET` is ≥32 random chars
- [ ] `RP_GITHUB_TOKEN` is a fine-grained PAT with `contents:write` only
      on the repos you actually want to review
- [ ] `RP_LLM_OPENAI_API_KEY` is stored in a secret manager (Vault / AWS
      Secrets Manager / k8s Secret), not a `.env` file on disk
- [ ] TLS is terminated at a reverse proxy (nginx / caddy / traefik);
      ReviewPilot itself speaks plaintext HTTP and trusts the proxy to
      strip `X-Forwarded-Proto` (v0.4.0 does NOT consume
      `X-Forwarded-*` headers — production deployments should bind to
      `127.0.0.1` and front it with the proxy)
- [ ] SQLite file is on encrypted disk (LUKS / AWS EBS encryption /
      k8s `storageClass: encrypted`)