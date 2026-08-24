# ReviewPilot

> Self-hosted AI code review bot (Go + CLI + Web UI). Ship v0.4.0 — MVP complete.

[中文版本](README.zh-CN.md)

ReviewPilot watches GitHub Pull Requests, runs a 5-step review pipeline
against an OpenAI-compatible LLM, posts inline + summary comments back
to GitHub, and gives the team a web UI to triage findings and feed
feedback back into the next run.

| Component | Status |
|---|---|
| `internal/llmopenai` | ✅ shipped (v0.1.0) |
| `internal/reviewengine` | ✅ shipped (v0.2.0) |
| `internal/platformgithub` | ✅ shipped (v0.3.0) |
| `internal/webdashboard` | ✅ shipped (v0.4.0) |
| CLI (`reviewpilot review`) | ✅ shipped (v0.3.0) |
| Docker image | ✅ shipped (v0.0.1, rebuilt per release) |
| GitHub Actions CI | ✅ green on `main` (134 tests) |

See [`CHANGELOG.md`](CHANGELOG.md) for what changed in each release.

## Why

You can already run **CodeRabbit** on GitHub, but it's a closed-source
SaaS. If you want a code-review bot that:

- Runs **on your own servers** (your code never leaves your VPC)
- Lets you **swap the LLM** to any OpenAI-compatible endpoint (DeepSeek,
  Moonshot, Zhipu, local Ollama, etc.)
- Stores review state in **a single SQLite file** you can back up
- Lets the team **upvote/downvote** comments and feed that signal back
  into future reviews

…then ReviewPilot is the smallest thing that does that.

## Quick start (Docker Compose)

```yaml
# /path/to/reviewpilot/docker-compose.yml
services:
  reviewpilot:
    build: .
    image: bbbbbmy/reviewpilot:v0.4.0
    container_name: reviewpilot
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      # Required — the bot won't start without these.
      RP_GITHUB_WEBHOOK_SECRET: "your-webhook-secret"
      RP_GITHUB_TOKEN: "ghp_xxx"
      RP_ADMIN_USER: admin
      RP_ADMIN_PASS: "your-admin-password"

      # Optional but recommended.
      RP_HTTP_LISTEN: ":8080"
      RP_DB_PATH: /var/lib/reviewpilot/review.db
      RP_LLM_OPENAI_API_KEY: "sk-xxx"
      # RP_LLM_OPENAI_BASE_URL: "https://api.deepseek.com/v1"   # for non-OpenAI providers
    volumes:
      - data:/var/lib/reviewpilot
    healthcheck:
      test: ["CMD", "/usr/local/bin/reviewpilot", "--healthcheck-only"]
      interval: 30s
      timeout: 5s
      retries: 3

volumes:
  data:
```

Then:

```bash
docker compose up -d
curl http://localhost:8080/healthz
# {"status":"ok"}
```

Open `http://localhost:8080` in a browser. You'll be prompted for the
Basic Auth credentials you set above.

For full setup walkthrough (TLS, reverse proxy, GitHub App registration,
production secrets management), see [`docs/deployment.md`](docs/deployment.md).

## CLI: review a git range locally

```bash
# Build the binary
go build -o bin/reviewpilot ./cmd/reviewpilot

# Review main..feature against your configured provider.
export RP_LLM_OPENAI_API_KEY="sk-..."
./bin/reviewpilot review main..feature --repo bbbbbmy/reviewpilot --out out.json

# Read patch from stdin (useful in CI).
git diff main..feature | ./bin/reviewpilot review --stdin --repo bbbbbmy/reviewpilot
```

The output is JSON:

```json
{
  "status": "done",
  "comments": [
    {
      "file_path": "internal/x.go",
      "line_number": 42,
      "body": "this nil-check is redundant after the early return",
      "fingerprint": "...",
      "posted_to_platform": false
    }
  ],
  "tokens_in": 200,
  "tokens_out": 30
}
```

CLI mode **does not post** to GitHub — it just runs the pipeline and
prints comments. To drive GitHub comments, run the server and send
webhooks.

## Server mode

`./bin/reviewpilot server` (the default if you run `./bin/reviewpilot`
with no subcommand) starts an HTTP server on `:8080` that listens for
GitHub `pull_request` webhooks.

| Path | Auth | Description |
|---|---|---|
| `POST /webhook` | GitHub HMAC signature | Receives PR events, runs the review pipeline, posts inline + summary comments |
| `GET /healthz` | none | Liveness check (`{"status":"ok"}`) |
| `GET /metrics` | none | Prometheus exposition (review-pipeline counters + histograms) |
| `GET /` | Basic Auth | Redirects to `/reviews` |
| `GET /reviews` | Basic Auth | HTML list of recent reviews |
| `GET /reviews/{id}` | Basic Auth | HTML detail with comments + 👍/👎 buttons |
| `GET /repos` | Basic Auth | HTML list of configured repos + upsert form |
| `POST /repos` | Basic Auth | Create / update repo config |
| `POST /reviews/{id}/comments/{cid}/feedback` | Basic Auth | Submit 👍/👎 + text |
| `GET /api/v1/repos` | Basic Auth | JSON list (for CLI / 3rd-party) |
| `POST /api/v1/repos` | Basic Auth | JSON upsert |
| `GET /api/v1/reviews` | Basic Auth | JSON list (filter by `?repo=` and `?status=`) |
| `GET /api/v1/reviews/{id}` | Basic Auth | JSON detail with comments |
| `POST /api/v1/reviews/{id}/comments/{cid}/feedback` | Basic Auth | JSON feedback submission |

The HTTP API is stable under `/api/v1/...` for as long as the major
version is `0`; breaking changes will require a new major.

## Configuration

| Env var | Required | Description |
|---|---|---|
| `RP_ADMIN_USER` / `RP_ADMIN_PASS` | yes | Basic Auth credentials for the dashboard + API. **Server refuses to start without them** (or `RP_DEV_MODE=1` to skip). |
| `RP_GITHUB_WEBHOOK_SECRET` | yes (server) | Shared secret used by GitHub to sign `X-Hub-Signature-256` on incoming webhooks. |
| `RP_GITHUB_TOKEN` | yes (server) | Bearer token for outbound GitHub API calls. Use a fine-grained PAT scoped to the repos you want to review, or a GitHub App installation token. |
| `RP_GITHUB_APP_ID` / `RP_GITHUB_APP_PRIVATE_KEY` | reserved | Reserved for GitHub App JWT exchange (full implementation in a follow-up). |
| `RP_GITHUB_BASE_URL` | no | Defaults to `https://api.github.com`. Override for GitHub Enterprise. |
| `RP_LLM_OPENAI_API_KEY` | yes | Provider API key. |
| `RP_LLM_OPENAI_BASE_URL` | no | Defaults to OpenAI. Set to `https://api.deepseek.com/v1`, `https://api.moonshot.cn/v1`, etc. |
| `RP_LLM_OPENAI_MODEL` | no | Defaults to `gpt-4o-mini`. |
| `RP_HTTP_LISTEN` | no | Default `:8080`. |
| `RP_DB_PATH` | no | Default `/var/lib/reviewpilot/review.db`. |
| `RP_DEV_MODE` | dev only | Set to `1` to skip Basic Auth (don't use in production). |

See [`docs/configuration.md`](docs/configuration.md) for full details
including recommended defaults and security notes.

## Architecture (1-minute tour)

```
                ┌──────────────────┐
                │  GitHub webhook  │
                │   (PR opened /   │
                │ synchronize)     │
                └─────────┬────────┘
                          │ HMAC signature
                          ▼
┌─────────────────────────────────────────────────┐
│           ReviewPilot server (:8080)            │
│                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐   │
│  │ cmd/      │───▶│ review-  │───▶│ llmopenai│   │
│  │ reviewpilot│    │ engine   │    │ OpenAI   │   │
│  └─────┬────┘    │ pipeline │    │ provider │   │
│        │         └─────┬────┘    └─────┬────┘   │
│        │               │                │       │
│        │               │ dedup          │       │
│        │               ▼                ▼       │
│        │         ┌──────────┐         token  │
│        │         │ platform-│          usage   │
│        │         │ github   │                  │
│        │         │ adapter  │                  │
│        │         └─────┬────┘                  │
│        ▼               ▼                       │
│  ┌──────────┐    ┌──────────┐                  │
│  │ webdash- │    │ SQLite   │                  │
│  │ board    │◀──▶│ store    │                  │
│  │ (HTML +  │    │ (repo /  │                  │
│  │  API +   │    │ review / │                  │
│  │  auth)   │    │ comment /│                  │
│  └─────┬────┘    │ feedback)│                  │
│        │         └──────────┘                  │
│        ▼                                        │
│  ┌──────────┐                                   │
│  │ Basic    │                                   │
│  │ Auth     │                                   │
│  └──────────┘                                   │
└─────────────────────────────────────────────────┘
```

The full architecture (component boundaries, decision rationale,
alternatives considered) lives in [`docs/architecture.md`](docs/architecture.md).

## Development

```bash
# Clone & install toolchain
go version  # 1.25 or newer required (go.mod declares 1.25.0)

# Run the full check set
gofmt -l .                                  # → empty
go vet ./...                                # → empty
go install honnef.co/go/tools/cmd/staticcheck@v0.7.0
staticcheck ./...                           # → empty
go test -count=1 -race ./...                # 134 tests, ~1s
```

GitHub Actions runs the same set on every push to `main`. See
`.github/workflows/ci.yml`.

## Project layout

```
bbbbbmy-reviewpilot/
├── cmd/reviewpilot/        # cobra root + server + CLI subcommand entry
├── internal/
│   ├── llmopenai/          # OpenAI-compatible LLM client + error taxonomy
│   ├── reviewengine/       # 5-step pipeline + dedup + feedback + metrics
│   ├── platformgithub/     # GitHub App webhook + adapter + rate-limit
│   └── webdashboard/       # HTML + JSON API + auth + SQLite store
│       ├── migrations/     # embedded SQL schema
│       ├── templates/      # html/template HTML
│       └── static/         # CSS
├── docs/                   # architecture / deployment / config / etc.
├── Dockerfile              # multi-stage → distroless static-debian12
├── docker-compose.yml      # one-file deploy
├── .github/workflows/      # lint / test / docker build
└── CHANGELOG.md            # release history
```

The behaviour contract for every package lives in
[`vibecoding → openspec/specs/`](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs).
If you change behaviour, change the spec first via an OpenSpec proposal.

## Operational docs

- [`docs/architecture.md`](docs/architecture.md) — data flow, component boundaries, decision table
- [`docs/configuration.md`](docs/configuration.md) — every env var in detail
- [`docs/deployment.md`](docs/deployment.md) — production deploy, TLS, reverse proxy, backups
- [`docs/cli.md`](docs/cli.md) — CLI flags, stdin, JSON output schema
- [`docs/webhook-setup.md`](docs/webhook-setup.md) — register a GitHub App + connect it to a repo
- [`docs/operations.md`](docs/operations.md) — backup / upgrade / monitor / troubleshoot

## License

[MIT](LICENSE)