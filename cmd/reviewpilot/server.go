package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
	"github.com/bbbbbmy/reviewpilot/internal/platformgithub"
	"github.com/bbbbbmy/reviewpilot/internal/reviewengine"
	"github.com/bbbbbmy/reviewpilot/internal/webdashboard"
)

// buildWebhookHandlerWithRegistry wires up the webhook HTTP handler
// using a shared Prometheus registry (so review metrics surface on
// /metrics). Returns (nil, error) on missing creds so main can serve
// a 503.
func buildWebhookHandlerWithRegistry(logger *slog.Logger, registry *prometheus.Registry) (http.Handler, error) {
	gh, err := platformgithub.NewAdapterFromEnv()
	if err != nil {
		return nil, err
	}
	eng, err := buildEngine(logger, registry)
	if err != nil {
		return nil, err
	}
	return makeWebhookHandler(logger, gh, eng), nil
}

// buildEngine constructs the review engine using the env-loaded
// GitHub adapter, the supplied Prometheus registry, and the
// SQLite-backed feedback store.
func buildEngine(logger *slog.Logger, registry *prometheus.Registry) (*reviewengine.DefaultEngine, error) {
	gh, err := platformgithub.NewAdapterFromEnv()
	if err != nil {
		return nil, err
	}
	provider, err := buildLLMProvider()
	if err != nil {
		return nil, err
	}
	dbPath := os.Getenv("RP_DB_PATH")
	if dbPath == "" {
		dbPath = "/var/lib/reviewpilot/review.db"
	}
	store, err := webdashboard.Open(dbPath)
	if err != nil {
		return nil, err
	}
	return reviewengine.NewDefaultEngine(reviewengine.EngineDeps{
		LLM:                 provider,
		Platform:            gh,
		Feedback:            store,
		Metrics:             reviewengine.NewMetrics(registry),
		MaxInputTokensPerPR: 100_000,
		FeedbackWindowDays:  30,
		Logger:              logger,
	})
}

// makeWebhookHandler returns the http.Handler that verifies the
// GitHub signature, runs the engine, and posts comments.
func makeWebhookHandler(logger *slog.Logger, gh *platformgithub.GitHubAdapter, eng reviewengine.Engine) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		sig := r.Header.Get("X-Hub-Signature-256")
		ev, err := platformgithub.VerifyAndParse(os.Getenv("RP_GITHUB_WEBHOOK_SECRET"), body, sig)
		if err != nil {
			logger.Warn("webhook signature verify failed", "err", err)
			http.Error(w, "signature invalid", http.StatusUnauthorized)
			return
		}
		patch, _ := extractPatch(body)
		res, err := eng.Run(r.Context(), reviewengine.Request{
			RepoID:    ev.RepoFullName,
			PRNumber:  ev.PRNumber,
			CommitSHA: ev.CommitSHA,
			Diff:      patch,
		})
		if err != nil {
			logger.Error("engine run failed", "err", err)
			http.Error(w, "engine failed", http.StatusInternalServerError)
			return
		}
		ctx := r.Context()
		posted := 0
		for _, c := range res.Comments {
			if c.LineNumber == 0 {
				continue
			}
			ic := platformgithub.Comment{Path: c.FilePath, Line: c.LineNumber, Body: c.Body}
			if err := gh.PostInlineComment(ctx, ev.RepoFullName, ev.PRNumber, ev.CommitSHA, ic); err != nil {
				logger.Warn("post inline failed", "err", err)
				continue
			}
			posted++
		}
		if summary := formatSummary(res); summary != "" {
			if err := gh.PostSummaryComment(ctx, ev.RepoFullName, ev.PRNumber, summary); err != nil {
				logger.Warn("post summary failed", "err", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":          string(res.Status),
			"comments_posted": posted,
			"tokens_in":       res.TokenUsage.TokensIn,
			"tokens_out":      res.TokenUsage.TokensOut,
		})
	})
}

// extractPatch pulls the `pull_request.patch` field from the webhook
// payload. Returns an empty string if missing or unparseable.
func extractPatch(body []byte) (string, error) {
	var raw struct {
		PullRequest struct {
			Patch string `json:"patch"`
			Diff  string `json:"diff"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", err
	}
	if raw.PullRequest.Patch != "" {
		return raw.PullRequest.Patch, nil
	}
	return raw.PullRequest.Diff, nil
}

// formatSummary produces a 1-line summary string for the run, or ""
// if there's nothing to summarize.
func formatSummary(res reviewengine.Result) string {
	if res.Status != reviewengine.StatusDone && res.Status != reviewengine.StatusSkipped {
		return ""
	}
	if len(res.Comments) == 0 && res.Reason == "" {
		return ""
	}
	s := "🤖 ReviewPilot ran (" + string(res.Status) + ")"
	if res.Reason != "" {
		s += " — " + res.Reason
	}
	if len(res.Comments) > 0 {
		s += " — posted comments"
	}
	return s
}

// silence unused imports on build tags etc.
var _ = context.Background
var _ = llmopenai.ErrTokenLimitExceeded
