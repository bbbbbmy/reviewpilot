package reviewengine

import (
	"context"
	"errors"
	"strings"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
)

// StepDecision is the engine's per-step action when a step fails.
type StepDecision string

const (
	// DecisionRetry indicates the engine should retry the failed step
	// (possibly with adjusted inputs, e.g. context_too_long → split
	// smaller chunks).
	DecisionRetry StepDecision = "retry"
	// DecisionSkip indicates the engine should abandon this review
	// cleanly with Status=Skipped; the webhook process stays alive.
	DecisionSkip StepDecision = "skip"
	// DecisionEscalate indicates a fatal config error (e.g., 401 from
	// upstream) that the operator must address.
	DecisionEscalate StepDecision = "escalate"
)

// Classify turns an error from a pipeline step into a StepDecision.
//
// Decision table (matches design.md §P3D6):
//
//	LLMError{Kind: context_too_long}      → retry (split smaller)
//	LLMError{Kind: rate_limited}          → retry after RetryAfter
//	LLMError{Kind: provider_5xx}          → skip
//	LLMError{Kind: provider_4xx}          → escalate (config error)
//	ErrTokenLimitExceeded                 → skip
//	context.DeadlineExceeded              → skip
//	default                              → skip
func Classify(err error) StepDecision {
	if err == nil {
		return DecisionRetry
	}

	var le *llmopenai.LLMError
	if errors.As(err, &le) {
		switch le.Kind {
		case llmopenai.KindContextTooLong:
			return DecisionRetry
		case llmopenai.KindRateLimited:
			return DecisionRetry
		case llmopenai.KindProvider4xx:
			return DecisionEscalate
		case llmopenai.KindProvider5xx, llmopenai.KindUnknown:
			return DecisionSkip
		}
	}

	if errors.Is(err, llmopenai.ErrTokenLimitExceeded) {
		return DecisionSkip
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return DecisionSkip
	}

	// Catch-all: default to skip to keep the webhook process alive.
	// Anything truly fatal should be caught at the type level above.
	return DecisionSkip
}

// ShortReason extracts a short, human-readable reason string for a
// failure (used in Result.Reason).
func ShortReason(err error) string {
	if err == nil {
		return ""
	}
	var le *llmopenai.LLMError
	if errors.As(err, &le) {
		if le.Message != "" {
			return le.Message
		}
		return string(le.Kind)
	}
	msg := err.Error()
	if i := strings.IndexAny(msg, ":\n"); i > 0 && i < 80 {
		return msg[:i]
	}
	if len(msg) > 80 {
		return msg[:80]
	}
	return msg
}
