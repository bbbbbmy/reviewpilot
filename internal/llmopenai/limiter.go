package llmopenai

import (
	"errors"
	"fmt"
)

// ErrTokenLimitExceeded is returned by Limiter.Check when an upstream
// request would exceed the per-PR input token budget.
var ErrTokenLimitExceeded = errors.New("llmopenai: input token budget exceeded")

// ApproxTokensPerChar is the rough conversion used for input sizing.
// English + code mixed text averages ~4 characters per token.
const ApproxTokensPerChar = 0.25

// Limiter rejects requests whose estimated input tokens exceed a
// configurable budget. Pre-flight at the call site avoids wasted
// HTTP round-trips and protects against pathological PR sizes.
type Limiter struct {
	// MaxInputTokens is the per-PR ceiling. Required.
	MaxInputTokens int
}

// Check estimates input tokens for the request and returns
// ErrTokenLimitExceeded when the ceiling would be breached.
//
// The estimate is intentionally conservative (30% buffer): it uses
// ApproxTokensPerChar and only counts role+content material —
// upper-bound figures (assistant prefill) — without relying on
// model-specific tokenizers.
func (l *Limiter) Check(req ChatRequest) error {
	if l.MaxInputTokens <= 0 {
		return errors.New("llmopenai: Limiter.MaxInputTokens must be positive")
	}
	est := estimateInputTokens(req)
	if est > l.MaxInputTokens {
		return fmt.Errorf("%w: estimated=%d, budget=%d",
			ErrTokenLimitExceeded, est, l.MaxInputTokens)
	}
	return nil
}

// estimateInputTokens sums character-length × ApproxTokensPerChar
// across all messages, then applies a 1.3× safety multiplier.
func estimateInputTokens(req ChatRequest) int {
	var chars int
	for _, m := range req.Messages {
		chars += len(m.Role) + len(m.Content)
	}
	tokens := int(float64(chars) * ApproxTokensPerChar)
	// 30% conservative buffer for splitter overhead and tool prompts
	// the caller may layer on top.
	tokens = tokens * 13 / 10
	return tokens
}
