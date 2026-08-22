package llmopenai

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestScrub_RedactsAuthorization(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ScrubbingHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("auth",
		"authorization", "Bearer sk-abcdef0123456789",
		"api_key", "sk-zyxw9876",
	)

	out := buf.String()
	if strings.Contains(out, "sk-abcdef0123456789") {
		t.Fatalf("authorization key leaked: %s", out)
	}
	if strings.Contains(out, "sk-zyxw9876") {
		t.Fatalf("api_key leaked: %s", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("redacted marker missing: %s", out)
	}
}

func TestScrub_RedactsBearerValueOnUnknownKey(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ScrubbingHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("headers", "headers", "Authorization: Bearer sk-abcdef0123456789")

	out := buf.String()
	if strings.Contains(out, "sk-abcdef0123456789") {
		t.Fatalf("leaked sk- token from non-keyed attr: %s", out)
	}
	if !strings.Contains(out, "<redacted>") {
		t.Fatalf("expected <redacted>, got %s", out)
	}
}

func TestScrub_LeavesUnrelatedAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ScrubbingHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("ok", "model", "gpt-4o-mini", "tokens", 42)

	out := buf.String()
	if !strings.Contains(out, "gpt-4o-mini") {
		t.Fatalf("lost model attr: %s", out)
	}
	if !strings.Contains(out, "42") {
		t.Fatalf("lost tokens attr: %s", out)
	}
}

func TestScrub_WithAttrsAtConstruction(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ScrubbingHandler(slog.NewJSONHandler(&buf, nil))).
		With("api_key", "sk-bad-token")

	logger.InfoContext(context.Background(), "hi")

	out := buf.String()
	if strings.Contains(out, "sk-bad-token") {
		t.Fatalf("redaction missed With() attrs: %s", out)
	}
}

func TestScrub_NonStringValuesScrubbableKeyStays(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(ScrubbingHandler(slog.NewJSONHandler(&buf, nil)))

	// non-string value on api_key key — we still redact the key,
	// because the *presence* of the key is enough signal.
	logger.Info("x", "api_key", 42)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(rec["api_key"].(string), "<redacted>") {
		t.Fatalf("expected redacted, got %v", rec["api_key"])
	}
}
