package platformgithub

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// parseRetryAfter accepts `Retry-After: <seconds>` or HTTP-date.
// Returns 0 if missing or unparseable.
func parseRetryAfter(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("retry-after"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
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

// classifyHTTPError maps a non-2xx response into a PlatformError.
func classifyHTTPError(op string, status int, header http.Header) *PlatformError {
	if status == http.StatusTooManyRequests {
		return &PlatformError{
			Op:         op,
			Kind:       kindRateLimited,
			StatusCode: status,
			Message:    "rate limited",
			RetryAfter: header.Get("retry-after"),
		}
	}
	if rem := header.Get("x-ratelimit-remaining"); rem == "0" {
		return &PlatformError{
			Op:         op,
			Kind:       kindRateLimited,
			StatusCode: status,
			Message:    "rate limited (x-ratelimit-remaining=0)",
			RetryAfter: header.Get("retry-after"),
		}
	}
	if status >= 500 && status < 600 {
		return &PlatformError{Op: op, Kind: kindProvider5xx, StatusCode: status, Message: "provider 5xx"}
	}
	if status >= 400 && status < 500 {
		return &PlatformError{Op: op, Kind: kindProvider4xx, StatusCode: status, Message: "provider 4xx"}
	}
	return &PlatformError{Op: op, Kind: kindUnknown, StatusCode: status, Message: "unknown status"}
}
