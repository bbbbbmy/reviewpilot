# Changelog

All notable changes to this project will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/), and this
project adheres to [Semantic Versioning](https://semver.org/).

## [v0.0.1] - 2026-08-22

### Added — Vertical skeleton (phase 1 of `implement-reviewpilot-mvp`)

Vertical skeleton of ReviewPilot: HTTP server with `/healthz`, module /
toolchain / packaging / CI scaffolding. Does not yet expose review /
LLM / GitHub / Web UI logic — those land in subsequent phases per the
implementation proposal. No spec changes; behaviour contract is locked
in [vibecoding → add-reviewpilot](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/archive/2026-08-22-add-reviewpilot).

- Module: `github.com/bbbbbmy/reviewpilot`, Go 1.22+
- Entrypoint: `cmd/reviewpilot/main.go` (graceful shutdown, slog, configurable
  via `RP_HTTP_LISTEN`)
- HTTP: `internal/webdashboard/server.go` (stdlib `net/http` with Go 1.22
  `ServeMux` enhanced path pattern; one endpoint: `GET /healthz`)
- Tests: `internal/webdashboard/server_test.go` (httptest, table-driven)
- Packaging:
  - `Dockerfile` — multi-stage `golang:1.22-bookworm` →
    `gcr.io/distroless/static-debian12:nonroot`
  - `docker-compose.yml` — single service, SQLite volume mount placeholder
- CI / hygiene:
  - `.github/workflows/ci.yml` — `lint` / `test` / `docker` jobs
  - `.github/dependabot.yml` — `gomod` + `github-actions`
  - `.golangci.yml` — errcheck / govet / staticcheck / unused / ineffassign / typecheck
  - `.editorconfig`, `.gitignore`, `LICENSE` (MIT)

### Implementation reference

- Implementation proposal: [vibecoding → implement-reviewpilot-mvp](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/implement-reviewpilot-mvp)
- Spec archive (locked contract): [vibecoding → 2026-08-22-add-reviewpilot](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/archive/2026-08-22-add-reviewpilot)
