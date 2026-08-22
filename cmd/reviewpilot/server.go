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
)

// buildWebhookHandler wires up the webhook HTTP handler. If creds
// are missing, returns (nil, error) so main can serve a 503.
//
// Returns (handler, nil) on success. The handler:
//   - verifies HMAC signature
//   - parses the GitHub pull_request event
//   - reads the diff payload (from JSON `pull_request.patch` if available)
//   - runs the engine
//   - posts inline + summary comments back to GitHub
func buildWebhookHandler(logger *slog.Logger) (http.Handler, error) {
	gh, err := platformgithub.NewAdapterFromEnv()
	if err != nil {
		return nil, err
	}

	provider, err := buildLLMProvider()
	if err != nil {
		return nil, err
	}

	// Default registry so the server's /metrics endpoint can include
	// review-pipeline metrics. The webdashboard's own Metrics counters
	// (HTTP) live separately in webdashboard.
	registry := prometheus.NewRegistry()

	eng, err := reviewengine.NewDefaultEngine(reviewengine.EngineDeps{
		LLM:                 provider,
		Platform:            gh,
		Feedback:            reviewengine.NewInMemoryFeedbackStore(),
		Metrics:             reviewengine.NewMetrics(registry),
		MaxInputTokensPerPR: 100_000,
		FeedbackWindowDays:  30,
		Logger:              logger,
	})
	if err != nil {
		return nil, err
	}

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

		// Extract diff from the payload. GitHub sends `pull_request.patch`
		// for PR webhooks but truncates very large diffs (> 65k chars).
		// MVP: use whatever the payload contains; fall back to "no diff".
		patch, err := extractPatch(body)
		if err != nil {
			logger.Warn("extract patch", "err", err)
		}

		req := reviewengine.Request{
			RepoID:    ev.RepoFullName,
			PRNumber:  ev.PRNumber,
			CommitSHA: ev.CommitSHA,
			Diff:      patch,
		}

		res, err := eng.Run(r.Context(), req)
		if err != nil {
			logger.Error("engine run failed", "err", err)
			http.Error(w, "engine failed", http.StatusInternalServerError)
			return
		}

		// Post comments. Inline + a summary if non-empty.
		ctx := r.Context()
		posted := 0
		for _, c := range res.Comments {
			if c.LineNumber == 0 {
				continue // skip "summary"-typed candidates — handled below
			}
			ic := platformgithub.Comment{Path: c.FilePath, Line: c.LineNumber, Body: c.Body}
			if err := gh.PostInlineComment(ctx, ev.RepoFullName, ev.PRNumber, ev.CommitSHA, ic); err != nil {
				logger.Warn("post inline failed", "err", err)
				continue
			}
			posted++
		}
		// One summary comment that aggregates the run.
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
	}), nil
}

// extractPatch pulls the `pull_request.patch` field from the webhook
// payload. Returns an empty string if missing or unparseable; the
// engine will treat empty as "nothing to do".
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

// silence unused import on context when only referenced in buildWebhookHandler
var _ = context.Background

// keep llmopenai import even if buildWebhookHandler inlines the provider build.
var _ = llmopenai.ErrTokenLimitExceeded
