package llmopenai

import (
	"context"
	"log/slog"
	"strings"
)

// redactMarker is what we replace sensitive values with.
const redactMarker = "<redacted>"

// scrubbableAttrKeys lists the attribute keys whose values are
// always redacted, case-insensitive.
var scrubbableAttrKeys = []string{
	"authorization",
	"api_key",
	"api-key",
	"x-api-key",
	"bearer",
}

// scrubbableValuePrefixes lists value prefixes that trigger
// redaction even when the key is unexpected (e.g., logs that
// concatenate "Bearer sk-..." into a generic "headers" attr).
//
// We additionally substring-match "bearer " in `isScrubbableValue`
// so values like "Authorization: Bearer sk-..." get caught even when
// the bearer token is not at the start of the value.
var scrubbableValuePrefixes = []string{
	"sk-",
}

// ScrubbingHandler returns an slog.Handler that wraps inner and
// redacts the values of scrubbable attribute keys (case-insensitive)
// plus values starting with "Bearer sk-…".
func ScrubbingHandler(inner slog.Handler) slog.Handler {
	return &scrubHandler{inner: inner}
}

type scrubHandler struct{ inner slog.Handler }

func (h *scrubHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *scrubHandler) Handle(ctx context.Context, r slog.Record) error {
	// Walk the record's attrs and replace scrubbable ones.
	redacted := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		if isScrubbableKey(a.Key) {
			a.Value = slog.StringValue(redactMarker)
		} else if a.Value.Kind() == slog.KindString && isScrubbableValue(a.Value.String()) {
			a.Value = slog.StringValue(redactMarker)
		}
		redacted = append(redacted, a)
		return true
	})
	cloned := slog.Record{
		Time:    r.Time,
		Message: r.Message,
		Level:   r.Level,
		PC:      r.PC,
	}
	cloned.AddAttrs(redacted...)
	return h.inner.Handle(ctx, cloned)
}

func (h *scrubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		if isScrubbableKey(a.Key) {
			a.Value = slog.StringValue(redactMarker)
		} else if a.Value.Kind() == slog.KindString && isScrubbableValue(a.Value.String()) {
			a.Value = slog.StringValue(redactMarker)
		}
		out = append(out, a)
	}
	return &scrubHandler{inner: h.inner.WithAttrs(out)}
}

func (h *scrubHandler) WithGroup(name string) slog.Handler {
	return &scrubHandler{inner: h.inner.WithGroup(name)}
}

func isScrubbableKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range scrubbableAttrKeys {
		if k == s {
			return true
		}
	}
	return false
}

func isScrubbableValue(v string) bool {
	low := strings.ToLower(v)
	// Substring "bearer " covers values like
	//   "Authorization: Bearer sk-abcdef0123456789"
	if strings.Contains(low, "bearer ") {
		return true
	}
	for _, p := range scrubbableValuePrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}
