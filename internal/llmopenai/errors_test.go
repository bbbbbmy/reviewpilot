package llmopenai

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestClassify_HTTPContextTooLongWinsEvenFor400(t *testing.T) {
	body := openAIErrorBody{}
	body.Error.Code = "context_length_exceeded"
	body.Error.Message = "max tokens"
	e := classifyHTTPError(opChat, http.StatusBadRequest, http.Header{}, body)
	if e.Kind != KindContextTooLong {
		t.Fatalf("kind=%s, want %s", e.Kind, KindContextTooLong)
	}
}

func TestClassify_RateLimited_WithRetryAfterSeconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	e := classifyHTTPError(opChat, http.StatusTooManyRequests, h, openAIErrorBody{})
	if e.Kind != KindRateLimited {
		t.Fatalf("kind=%s", e.Kind)
	}
	if e.RetryAfter != 30*time.Second {
		t.Fatalf("RetryAfter=%v", e.RetryAfter)
	}
}

func TestClassify_RateLimited_HeaderRemainingZero(t *testing.T) {
	h := http.Header{}
	h.Set("x-ratelimit-remaining", "0")
	e := classifyHTTPError(opChat, http.StatusOK, h, openAIErrorBody{})
	if e.Kind != KindRateLimited {
		t.Fatalf("kind=%s, want %s", e.Kind, KindRateLimited)
	}
}

func TestClassify_5xx(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504} {
		e := classifyHTTPError(opChat, code, http.Header{}, openAIErrorBody{})
		if e.Kind != KindProvider5xx {
			t.Fatalf("status=%d kind=%s", code, e.Kind)
		}
	}
}

func TestClassify_Other4xx(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404} {
		e := classifyHTTPError(opChat, code, http.Header{}, openAIErrorBody{})
		if e.Kind != KindProvider4xx {
			t.Fatalf("status=%d kind=%s", code, e.Kind)
		}
	}
}

func TestClassify_Unknown(t *testing.T) {
	e := classifyHTTPError(opChat, 0, http.Header{}, openAIErrorBody{})
	if e.Kind != KindUnknown {
		t.Fatalf("kind=%s", e.Kind)
	}
}

func TestLLMError_ErrorString(t *testing.T) {
	e := &LLMError{Op: opChat, Kind: KindProvider5xx, StatusCode: 503, Message: "boom"}
	want := "llm.openai.chat: boom (kind=provider_5xx, status=503)"
	if got := e.Error(); got != want {
		t.Fatalf("Error()=%q want %q", got, want)
	}
}

func TestLLMError_Is(t *testing.T) {
	a := &LLMError{Op: opChat, Kind: KindRateLimited}
	b := &LLMError{Op: opStream, Kind: KindRateLimited, StatusCode: 429}
	c := &LLMError{Op: opChat, Kind: KindProvider5xx}

	if !errors.Is(a, b) {
		t.Fatalf("errors.Is(a, b) should be true (same Kind)")
	}
	if errors.Is(a, c) {
		t.Fatalf("errors.Is(a, c) should be false (different Kind)")
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	if got := parseRetryAfter(http.Header{}); got != 0 {
		t.Fatalf("RetryAfter=%v", got)
	}
	if got := parseRetryAfter(nil); got != 0 {
		t.Fatalf("nil header should be 0, got %v", got)
	}
}

func TestParseRetryAfter_BadValue(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "not-a-number")
	if got := parseRetryAfter(h); got != 0 {
		t.Fatalf("bad value should yield 0, got %v", got)
	}
}
