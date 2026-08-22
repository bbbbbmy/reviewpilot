# Changelog

All notable changes to this project will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/), and this
project adheres to [Semantic Versioning](https://semver.org/).

## [v0.1.0] - 2026-08-22

### Added — llm-providers package (`internal/llmopenai`)

Implements the [llm-providers capability](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/llm-providers) (6 Requirement). This is the foundation for the next phase (review-engine).

- `internal/llmopenai/types.go` — `LLMProvider` interface + `ChatRequest` / `ChatResponse` / `Message` / `Role` / `Usage` / `FinishReason`
- `internal/llmopenai/openai.go` — `OpenAICompatibleProvider`: `Chat(ctx, req)` + `ChatStream(ctx, req, onDelta)` (SSE); configurable `BaseURL` (so OpenAI / DeepSeek / Moonshot / etc. all work), `APIKey`, `Model`, `HTTPClient`
- `internal/llmopenai/errors.go` — `LLMError{Kind, StatusCode, RetryAfter}` + `Kind` enum: `rate_limited` / `context_too_long` / `provider_5xx` / `provider_4xx` / `unknown`; `errors.Is(err, &LLMError{Kind: ...})` supported
- `internal/llmopenai/limiter.go` — per-PR token-budget pre-flight (`Limiter.Check(req)`); conservative 1.3× buffer; rejects before HTTP if budget exceeded
- `internal/llmopenai/scrubber.go` — slog `ReplaceAttr` handler that redacts keys (`authorization` / `api_key` / …) and substring `bearer ` in any string value
- 33 unit tests covering: success / 401 / 429 + `Retry-After` / 400 context_length_exceeded / 5xx / malformed JSON / SSE streaming + 5xx-in-stream / comment lines / limiter small+big / scrubber key+value redaction

### Verification
- `gofmt -l .` — empty
- `go vet ./...` — empty
- `go test -count=1 ./...` — all green

### Implementation reference
- Phase 2 proposal: [vibecoding → add-reviewpilot-impl-phase2](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/add-reviewpilot-impl-phase2)
- Spec: [vibecoding → spec/llm-providers](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/llm-providers)

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
