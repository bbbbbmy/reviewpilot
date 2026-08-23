# Architecture

## Component map

```
                ┌──────────────────┐
                │  GitHub webhook  │
                │   (PR opened /   │
                │ synchronize)     │
                └─────────┬────────┘
                          │ HMAC signature (X-Hub-Signature-256)
                          ▼
┌─────────────────────────────────────────────────┐
│           ReviewPilot server (:8080)            │
│                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐   │
│  │ cmd/      │───▶│ review-  │───▶│ llmopenai│   │
│  │ reviewpilot│    │ engine   │    │ OpenAI   │   │
│  │ (cobra)   │    │ pipeline │    │ provider │   │
│  └─────┬────┘    └─────┬────┘    └─────┬────┘   │
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

## Data flow (one review run)

1. **GitHub** sends `pull_request` event to `POST /webhook` with body + `X-Hub-Signature-256`.
2. `cmd/reviewpilot/server.go::makeWebhookHandler` reads the body, verifies HMAC, parses the event.
3. The handler builds a `reviewengine.Request{RepoID, PRNumber, CommitSHA, Diff}` (Diff pulled from `pull_request.patch` in the payload — may be empty for very large diffs).
4. `reviewengine.DefaultEngine.Run(ctx, req)` executes the 5-step pipeline (see below).
5. For each surviving comment, the handler calls `platformgithub.GitHubAdapter.PostInlineComment(...)`.
6. Optionally, a one-line summary is posted via `PostSummaryComment(...)`.
7. The whole run (status, comments, feedback window) is persisted to `webdashboard.SQLiteStore`.
8. Prometheus counters/histograms are emitted under `/metrics`.

## 5-step pipeline (`internal/reviewengine`)

```
            ┌──────────┐
   split ──▶│ diff →   │
            │ chunks   │  file-level; max_input_tokens_per_pr × 60% safety
            └─────┬────┘
                  ▼
            ┌──────────┐
  context ──▶│ assemble │  system + per-repo feedback summary (30-day window)
            │ ChatReq  │  + per-file user message
            └─────┬────┘
                  ▼
            ┌──────────┐
     llm ──▶│ LLM      │  llmopenai.OpenAICompatibleProvider.Chat
            │ Chat     │  (or ChatStream for streaming)
            └─────┬────┘
                  ▼
            ┌──────────┐
    dedup ──▶│ platform │  sha256(normalized body) fingerprint +
            │ List +   │  token jaccard ≥ 0.85
            │ drop dup │
            └─────┬────┘
                  ▼
            ┌──────────┐
 feedback ──▶│ SQLite   │  Comments go to platformgithub for posting;
            │ persist  │  Runs persist for the dashboard.
            └──────────┘
```

Every step has its own timer; failures are funneled through
`failure.Classify(err)` → `Retry` / `Skip` / `Escalate`. Per-chunk
failures don't kill the whole review — webhook process stays alive
(per `specs/review-engine` "失败隔离" Requirement).

## Failure decision table

| Error                                     | Decision    | Action                       |
|-------------------------------------------|-------------|------------------------------|
| `LLMError{Kind: context_too_long}`        | `Retry`     | Split smaller; retry once     |
| `LLMError{Kind: rate_limited}`            | `Retry`     | Wait `RetryAfter`; retry once |
| `LLMError{Kind: provider_5xx}`            | `Skip`      | Skip this PR; webhook OK     |
| `LLMError{Kind: provider_4xx}`            | `Escalate`  | 401/403 → log + alert        |
| `llmopenai.ErrTokenLimitExceeded`          | `Skip`      | PR too large; skip           |
| `context.DeadlineExceeded`                | `Skip`      | Skip; webhook OK             |
| Anything else                             | `Skip`      | Conservative default         |

## Why a SQLite store?

The dashboard needs:
- Fast random-access reads (review detail page)
- Concurrent writes (webhook + UI feedback)
- Single-file backup (`cp review.db backup.db`)

SQLite via `modernc.org/sqlite` (pure Go, no CGO) gives all three
without operational overhead. WAL mode keeps writes from blocking
reads. We use a single process so write contention is bounded by HTTP
request concurrency (≪ 100 RPS in practice).

## Why stdlib `net/http`?

Go 1.22's `http.ServeMux` supports path patterns (`GET /reviews/{id}`),
which is enough for our routing. We avoid `chi`/`gin`/`echo` to keep
the binary small and the dependency tree shallow. The web UI uses
`html/template` + HTMX (loaded from CDN) — no React/Vue toolchain.

## Why stdlib `log/slog`?

Structured logging with zero deps. The `ScrubbingHandler` in
`llmopenai` redacts auth headers / API keys before they hit stdout/stderr.

## Why a custom CLI framework?

We use `spf13/cobra` only for the `review` and `server` subcommands.
Single-binary distribution; no runtime dependencies; works in CI without
Docker.

## Module-level dependency graph

```
cmd/reviewpilot      ──▶  webdashboard
                   ──▶  reviewengine ──▶  llmopenai
                                ──▶  platformgithub
                                ──▶  llmopenai
                                          ──▶  (stdlib net/http)
                    ──▶  webdashboard (FeedbackStore impl)
                    ──▶  prometheus/client_golang
                   ──▶  modernc.org/sqlite (via webdashboard)
                   ──▶  cobra
```

No cycles. Every internal package can be imported in isolation for tests.

## Out of scope (v0.4.0)

- GitLab / Gitea / self-hosted Git platform adapters
- Anthropic / Ollama / Gemini providers (interface exists; not implemented)
- Production OIDC SSO (`RP_OIDC_*` env vars reserved; no flow implemented)
- Multi-tenant / per-user RBAC (single admin role)
- Real GitHub App JWT exchange (use `RP_GITHUB_TOKEN` with a PAT or
  pre-minted installation token; JWT flow is a follow-up)

See `vibecoding → openspec/changes/` for the archived proposals that
drove each phase.