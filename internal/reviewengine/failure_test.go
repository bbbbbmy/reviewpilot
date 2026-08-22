package reviewengine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
)

func TestClassify_NilReturnsRetry(t *testing.T) {
	if got := Classify(nil); got != DecisionRetry {
		t.Fatalf("got=%s", got)
	}
}

func TestClassify_ContextTooLongIsRetry(t *testing.T) {
	err := &llmopenai.LLMError{Kind: llmopenai.KindContextTooLong}
	if got := Classify(err); got != DecisionRetry {
		t.Fatalf("got=%s", got)
	}
}

func TestClassify_RateLimitedIsRetry(t *testing.T) {
	err := &llmopenai.LLMError{Kind: llmopenai.KindRateLimited}
	if got := Classify(err); got != DecisionRetry {
		t.Fatalf("got=%s", got)
	}
}

func TestClassify_Provider5xxIsSkip(t *testing.T) {
	err := &llmopenai.LLMError{Kind: llmopenai.KindProvider5xx}
	if got := Classify(err); got != DecisionSkip {
		t.Fatalf("got=%s", got)
	}
}

func TestClassify_Provider4xxIsEscalate(t *testing.T) {
	err := &llmopenai.LLMError{Kind: llmopenai.KindProvider4xx}
	if got := Classify(err); got != DecisionEscalate {
		t.Fatalf("got=%s", got)
	}
}

func TestClassify_TokenLimitExceededIsSkip(t *testing.T) {
	if got := Classify(llmopenai.ErrTokenLimitExceeded); got != DecisionSkip {
		t.Fatalf("got=%s", got)
	}
}

func TestClassify_UnknownErrorIsSkip(t *testing.T) {
	if got := Classify(errors.New("random")); got != DecisionSkip {
		t.Fatalf("got=%s", got)
	}
}

func TestShortReason_LLMErrorMessage(t *testing.T) {
	err := &llmopenai.LLMError{Kind: llmopenai.KindContextTooLong, Message: "ctx too long"}
	if got := ShortReason(err); got != "ctx too long" {
		t.Fatalf("got=%q", got)
	}
}

func TestShortReason_PlainErrorFirstColon(t *testing.T) {
	err := errors.New("auth: invalid api key")
	if got := ShortReason(err); got != "auth" {
		t.Fatalf("got=%q", got)
	}
}

// --- feedback_store tests ---

func TestInMemoryFeedbackStore_AddAndRecent(t *testing.T) {
	s := NewInMemoryFeedbackStore()
	_ = s.Add(context.Background(), "r", Feedback{Rating: RatingUp, CreatedAt: time.Now().Add(-2 * 24 * time.Hour)})
	_ = s.Add(context.Background(), "r", Feedback{Rating: RatingDown, CreatedAt: time.Now()})

	// 7-day window: both entries in range.
	got, err := s.RecentForRepo(context.Background(), "r", 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}

	// 1-day window: only the recent one.
	got, err = s.RecentForRepo(context.Background(), "r", 1*24*time.Hour)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestInMemoryFeedbackStore_ReposAreIsolated(t *testing.T) {
	s := NewInMemoryFeedbackStore()
	_ = s.Add(context.Background(), "r1", Feedback{Rating: RatingUp, CreatedAt: time.Now()})
	_ = s.Add(context.Background(), "r2", Feedback{Rating: RatingDown, CreatedAt: time.Now()})

	got, _ := s.RecentForRepo(context.Background(), "r1", 30*24*time.Hour)
	if len(got) != 1 || got[0].Rating != RatingUp {
		t.Fatalf("r1=%v", got)
	}
	got, _ = s.RecentForRepo(context.Background(), "r2", 30*24*time.Hour)
	if len(got) != 1 || got[0].Rating != RatingDown {
		t.Fatalf("r2=%v", got)
	}
}

// guard that err interface satisfies for our assertions
var _ error = error(nil)
