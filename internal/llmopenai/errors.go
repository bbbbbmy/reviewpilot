package llmopenai

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Kind classifies an LLM-side error so callers can react (retry /
// split / skip).
type Kind string

const (
	KindRateLimited    Kind = "rate_limited"
	KindContextTooLong Kind = "context_too_long"
	KindProvider5xx    Kind = "provider_5xx"
	KindProvider4xx    Kind = "provider_4xx"
	KindUnknown        Kind = "unknown"
)

// LLMError is the canonical error produced by every LLMProvider
// implementation in this package.
type LLMError struct {
	Op         string        // chat completion step name (e.g. "openai.chat")
	Kind       Kind          // normalized classification
	StatusCode int           // HTTP status (0 if N/A)
	Message    string        // human-readable summary
	RetryAfter time.Duration // populated for KindRateLimited when the
	// provider advertises it
}

// Error implements the error interface.
func (e *LLMError) Error() string {
	if e == nil {
		return "<nil llm error>"
	}
	if e.Message != "" {
		return fmt.Sprintf("llm.%s: %s (kind=%s, status=%d)", e.Op, e.Message, e.Kind, e.StatusCode)
	}
	return fmt.Sprintf("llm.%s: kind=%s status=%d", e.Op, e.Kind, e.StatusCode)
}

// Unwrap returns nil (kept for future compatibility with errors.As).
func (e *LLMError) Unwrap() error { return nil }

// Is allows errors.Is(err, &LLMError{Kind: ...}) comparisons.
func (e *LLMError) Is(target error) bool {
	var t *LLMError
	if !errors.As(target, &t) {
		return false
	}
	return e.Kind == t.Kind
}

// classifyHTTPError maps an HTTP error response into a normalized
// LLMError. `body` is the parsed OpenAI error body when available.
//
// Precedence:
//  1. `error.code == "context_length_exceeded"` in body → KindContextTooLong
//     regardless of HTTP status.
//  2. 429 or `x-ratelimit-remaining: 0` header → KindRateLimited
//     (RetryAfter from Retry-After header when present).
//  3. 5xx → KindProvider5xx
//  4. Other non-2xx → KindProvider4xx
//  5. Unclassifiable → KindUnknown
func classifyHTTPError(op string, status int, header http.Header, body openAIErrorBody) *LLMError {
	// 1. Context too long wins (OpenAI returns 400 for this).
	if strings.EqualFold(body.Error.Code, "context_length_exceeded") {
		return &LLMError{
			Op:         op,
			Kind:       KindContextTooLong,
			StatusCode: status,
			Message:    fmt.Sprintf("context length exceeded: %s", body.Error.Message),
		}
	}

	// 2. Rate limited.
	if status == http.StatusTooManyRequests {
		return &LLMError{
			Op:         op,
			Kind:       KindRateLimited,
			StatusCode: status,
			Message:    "rate limited",
			RetryAfter: parseRetryAfter(header),
		}
	}
	if rem := header.Get("x-ratelimit-remaining"); rem == "0" || rem == "" && header.Get("x-ratelimit-reset") != "" {
		// Per OpenAI docs: when remaining hits zero, expect throttling.
		if rem == "0" {
			return &LLMError{
				Op:         op,
				Kind:       KindRateLimited,
				StatusCode: status,
				Message:    "rate limited (x-ratelimit-remaining=0)",
				RetryAfter: parseRetryAfter(header),
			}
		}
	}

	// 3. 5xx.
	if status >= 500 && status < 600 {
		return &LLMError{
			Op:         op,
			Kind:       KindProvider5xx,
			StatusCode: status,
			Message:    fmt.Sprintf("provider %d: %s", status, body.Error.Message),
		}
	}

	// 4. Other non-2xx.
	if status >= 400 && status < 500 {
		return &LLMError{
			Op:         op,
			Kind:       KindProvider4xx,
			StatusCode: status,
			Message:    fmt.Sprintf("client %d: %s", status, body.Error.Message),
		}
	}

	// 5. Unknown — caller passed a 2xx, or status is zero/negative.
	return &LLMError{
		Op:         op,
		Kind:       KindUnknown,
		StatusCode: status,
		Message:    body.Error.Message,
	}
}

// openAIErrorBody mirrors the relevant fields of an OpenAI error response.
type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// parseRetryAfter accepts either `Retry-After: <seconds>` or
// `Retry-After: <HTTP-date>` (RFC 7231 §7.1.3).
//
// Returns 0 when the header is missing, unparseable, or in the past.
func parseRetryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	v := h.Get("retry-after")
	if v == "" {
		return 0
	}
	if secs, err := time.ParseDuration(v + "s"); err == nil {
		if secs < 0 {
			return 0
		}
		return secs
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
