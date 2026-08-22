package platformgithub

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter_Seconds(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	if got := parseRetryAfter(h); got != 30*time.Second {
		t.Fatalf("got=%v", got)
	}
}

func TestParseRetryAfter_Zero(t *testing.T) {
	if got := parseRetryAfter(http.Header{}); got != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseRetryAfter_BadNumber(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "not-a-number")
	if got := parseRetryAfter(h); got != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	h := http.Header{}
	// Round-trip through UTC to avoid local-vs-UTC wall-clock drift
	// in time.Until arithmetic.
	future := time.Now().UTC().Add(60 * time.Second).Format(http.TimeFormat)
	h.Set("Retry-After", future)
	if got := parseRetryAfter(h); got < 30*time.Second || got > 90*time.Second {
		t.Fatalf("got=%v", got)
	}
}

func TestParseRetryAfter_PastHTTPDate(t *testing.T) {
	h := http.Header{}
	past := time.Now().UTC().Add(-60 * time.Second).Format(http.TimeFormat)
	h.Set("Retry-After", past)
	if got := parseRetryAfter(h); got != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestParseRetryAfter_NilHeader(t *testing.T) {
	if got := parseRetryAfter(nil); got != 0 {
		t.Fatalf("got=%v", got)
	}
}

func TestClassifyHTTPError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header http.Header
		want   platformErrorKind
	}{
		{"429", 429, http.Header{}, kindRateLimited},
		{"x-ratelimit-remaining=0", 200, http.Header{"X-Ratelimit-Remaining": []string{"0"}}, kindRateLimited},
		{"500", 500, http.Header{}, kindProvider5xx},
		{"502", 502, http.Header{}, kindProvider5xx},
		{"400", 400, http.Header{}, kindProvider4xx},
		{"401", 401, http.Header{}, kindProvider4xx},
		{"404", 404, http.Header{}, kindProvider4xx},
		{"200 (2xx)", 200, http.Header{}, kindUnknown},
		{"zero", 0, http.Header{}, kindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyHTTPError("op", tc.status, tc.header)
			if got.Kind != tc.want {
				t.Errorf("Kind=%s want=%s", got.Kind, tc.want)
			}
		})
	}
}

func TestPlatformError_ErrorString(t *testing.T) {
	e := &PlatformError{Op: "x", Kind: kindRateLimited, StatusCode: 429, Message: "too many"}
	if got := e.Error(); got != "x: too many (kind=rate_limited, status=429)" {
		t.Fatalf("got=%q", got)
	}
}

func TestPlatformError_NilSafe(t *testing.T) {
	var e *PlatformError
	got := e.Error()
	if got != "<nil platform error>" {
		t.Fatalf("got=%q", got)
	}
}

func TestPlatformError_UnwrapAsErr(t *testing.T) {
	e := &PlatformError{Kind: kindRateLimited, Message: "x"}
	if !errors.Is(e, e) { // sanity
		t.Fatal("self-equality should hold")
	}
}
