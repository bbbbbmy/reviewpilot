# Changelog

All notable changes to this project will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/), and this
project adheres to [Semantic Versioning](https://semver.org/).

## [v0.3.0] - 2026-08-22

### Added — platform-integrations package (`internal/platformgithub`)

Implements the [platform-integrations capability](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/platform-integrations) (4 Requirement). The real `GitHubAdapter` replaces the phase-3 fake platform; CLI + server entry points land.

- `types.go` — `Config{AppID, PrivateKeyPEM, WebhookSecret, Token, HTTPClient, BaseURL}` + `PullRequestEvent` + `Comment` + `PlatformError`
- `credentials.go` — `LoadConfigFromEnv()` reads `RP_GITHUB_*` env vars; missing required var fails fast
- `webhook.go` — `VerifySignature` (HMAC-SHA256 + constant-time compare) + `ParsePullRequestEvent` (distinguishes pull_request vs issue events by top-level field) + `VerifyAndParse` combo
- `ratelimit.go` — `parseRetryAfter` (seconds + HTTP-date) + `classifyHTTPError` (rate_limited / 5xx / 4xx / unknown)
- `comments.go` — `PostInlineComment` (review-comments API) + `PostSummaryComment` (issue-comments API) + `ListComments` (inline + summary, partial result on summary failure)
- `adapter.go` — `GitHubAdapter` implements `reviewengine.PlatformAdapter`; `NewAdapterFromEnv()` for server bootstrap
- 27 unit tests (httptest mock GitHub API)

### Added — CLI + Server entry (`cmd/reviewpilot`)

- `cli.go` — cobra `review` subcommand:
  - `reviewpilot review <base>...<head> --repo <id>` (git diff)
  - `reviewpilot review --stdin` (read patch from stdin)
  - `--out -` (stdout) / `--out <file>`
  - non-zero exit codes for missing repo / empty diff / git failure / engine failure
- `main.go` — cobra root + `review` / `server` / `version` subcommands
- `server.go` — webhook receiver:
  - HMAC signature verify → 401 on fail
  - parse PR event → diff from payload.patch (or payload.diff)
  - engine run → post inline comments + summary comment
  - failure isolation: webhook handler errors return 5xx, server process stays alive
  - 503 on /webhook if credentials missing

### Changed

- `internal/reviewengine`: `EngineDeps.Platform` is now optional (nil = CLI mode, no platform dedup); `NewDefaultEngine` no longer requires it
- `internal/reviewengine.Pipeline` nil-safe on `Platform`
- `cmd/reviewpilot/main.go` rewritten on top of cobra

### Dependencies

- `github.com/spf13/cobra v1.10.2` added (CLI framework)
- `github.com/spf13/pflag` (transitive)
- `github.com/inconshreveable/mousetrap` (transitive)

### Verification

- `gofmt -l .` empty
- `go vet ./...` empty
- `staticcheck ./...` empty
- `go test -count=1 ./...` — 107 tests pass

### Implementation reference

- Phase 4 proposal: [vibecoding → add-reviewpilot-impl-phase4](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/add-reviewpilot-impl-phase4)
- Spec: [vibecoding → spec/platform-integrations](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/platform-integrations)

## [v0.2.0] - 2026-08-22

### Added — review-engine package (`internal/reviewengine`)

Implements the [review-engine capability](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/review-engine) (6 Requirement). The orchestrator consumes the `LLMProvider` interface from phase 2.

- `types.go` — `Request` / `Result` / `Comment` / `Feedback` / `Status` / `StepName` / `TokenUsage`
- `engine.go` — `Engine` interface + `DefaultEngine` + `EngineDeps`
- `platform.go` — `PlatformAdapter` interface (consumer-side; phase 4 provides `GitHubAdapter`)
- `feedback_store.go` — `FeedbackStore` interface + `InMemoryFeedbackStore` (30-day window, 10k/repo cap, thread-safe)
- `pipeline.go` — 5-step orchestrator (split → context → LLM → dedup → feedback)
- `split.go` — file-level diff splitter with token estimation + 60% budget safety margin
- `dedup.go` — sha256 fingerprint + token-jaccard similarity (≥ 0.85 drops)
- `failure.go` — `Classify(err) StepDecision` table: context_too_long → retry; rate_limited → retry; provider_5xx → skip; provider_4xx → escalate; ErrTokenLimitExceeded → skip
- `metrics.go` — prometheus collectors (`reviewpilot_review_total{status}`, `reviewpilot_review_tokens_total{stage}`, `reviewpilot_review_step_duration_seconds{step}`); nil-safe for tests
- 33 unit tests covering: end-to-end happy path, dedup by exact FP, dedup by token similarity, feedback injection on second run, single-file-oversized → skipped, LLM 5xx failure isolation, missing dep validation

### Build environment

- Go toolchain upgraded to **1.25** (required by `github.com/prometheus/client_golang v1.24.1`)
- Dockerfile `golang:1.25-bookworm` (was `1.22-bookworm`)
- ci.yml `go-version: "1.25"` (was `"1.22"`)
- `go.mod` declares `go 1.25.0` + `toolchain go1.26.7`

### Verification

- `gofmt -l .` empty
- `go vet ./...` empty
- `staticcheck ./...` empty
- `go test -count=1 ./...` all green (69 tests total)

### Implementation reference

- Phase 3 proposal: [vibecoding → add-reviewpilot-impl-phase3](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/changes/add-reviewpilot-impl-phase3)
- Spec: [vibecoding → spec/review-engine](https://github.com/bbbbbmy/vibecoding/tree/main/openspec/specs/review-engine)

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
