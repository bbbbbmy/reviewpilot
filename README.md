# ReviewPilot

> Self-hosted AI code review bot (Go + CLI + Web UI). **Currently in vertical-skeleton (v0.0.1)** — only HTTP server + `/healthz` exist; review / LLM / GitHub / Web UI land in subsequent releases.

## Status

| Phase | Status | Notes |
|---|---|---|
| v0.0.1 — Vertical skeleton | ✅ this release | `GET /healthz` only |
| v0.1.0 — Core review loop | ⏳ next | LLM + GitHub webhook + review pipeline |
| v0.2.0 — Web UI | ⏳ later | config / records / feedback |

See the [Implementation Proposal](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/implement-reviewpilot-mvp) and the [Spec Archive (locked contract)](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/archive/2026-08-22-add-reviewpilot) for the full roadmap.

## Specification (locked behaviour contract)

4 capabilities / 22 Requirements are defined in the **[add-reviewpilot archive](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/archive/2026-08-22-add-reviewpilot/)**:

- [`review-engine`](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/review-engine) — review pipeline (6 Requirement)
- [`platform-integrations`](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/platform-integrations) — GitHub App + CLI (4 Requirement)
- [`llm-providers`](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/llm-providers) — OpenAI-compatible provider (6 Requirement)
- [`web-dashboard`](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/web-dashboard) — Web UI + HTTP API (6 Requirement)

Any change to user-visible behaviour MUST go through an OpenSpec proposal in
the [vibecoding](https://github.com/bbbbbmy/vibecoding) repo first.

## Quick start (dev)

Requires Go 1.22+.

```bash
# Build
go build -o ./bin/reviewpilot ./cmd/reviewpilot

# Run (default :8080)
./bin/reviewpilot

# Hit the healthcheck
curl -sS http://localhost:8080/healthz | jq
# => {"status":"ok"}

# Run tests
go test ./...
```

Override the listen address with `RP_HTTP_LISTEN=:<port>`.

## Quick start (production, Docker Compose)

```bash
docker compose up -d
curl -sS http://localhost:8080/healthz
```

The image is multi-stage: `golang:1.22-bookworm` → `gcr.io/distroless/static-debian12:nonroot`.

## License

[MIT](LICENSE)
