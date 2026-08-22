package llmopenai

import (
	"errors"
	"testing"
)

func TestLimiter_AllowsSmall(t *testing.T) {
	l := &Limiter{MaxInputTokens: 1000}
	req := ChatRequest{
		Model: "gpt-test",
		Messages: []Message{
			{Role: RoleSystem, Content: "short system"},
			{Role: RoleUser, Content: "hi"},
		},
	}
	if err := l.Check(req); err != nil {
		t.Fatalf("small request should pass: %v", err)
	}
}

func TestLimiter_RejectsBig(t *testing.T) {
	l := &Limiter{MaxInputTokens: 100}
	req := ChatRequest{
		Model: "gpt-test",
		Messages: []Message{
			{Role: RoleUser, Content: string(make([]byte, 4000))}, // ~1000 tokens raw
		},
	}
	err := l.Check(req)
	if err == nil {
		t.Fatalf("big request should be rejected")
	}
	if !errors.Is(err, ErrTokenLimitExceeded) {
		t.Fatalf("err not ErrTokenLimitExceeded: %v", err)
	}
}

func TestLimiter_ZeroBudgetIsMisconfig(t *testing.T) {
	l := &Limiter{MaxInputTokens: 0}
	err := l.Check(ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatalf("zero budget should error")
	}
}

func TestEstimateInputTokens_Buffer(t *testing.T) {
	// "user" role + 1 char content = 5 chars total ⇒ 1.25 ⇒ 1 token
	// raw, then ×1.3 buffer ⇒ still 1.
	req := ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}}
	got := estimateInputTokens(req)
	if got != 1 {
		t.Fatalf("estimate=%d want 1", got)
	}
	// 40 chars content + 4 chars role = 44 chars ⇒ 11 tokens raw ⇒ 14 with buffer.
	req2 := ChatRequest{Messages: []Message{{Role: RoleUser, Content: string(make([]byte, 40))}}}
	got2 := estimateInputTokens(req2)
	if got2 < 13 || got2 > 15 {
		t.Fatalf("estimate=%d want ~14", got2)
	}
}
