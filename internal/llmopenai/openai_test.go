package llmopenai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewProvider_RequiresAPIKey(t *testing.T) {
	_, err := NewProvider(Config{})
	if err == nil {
		t.Fatalf("expected error on empty APIKey")
	}
}

func TestNewProvider_DefaultsBaseURLAndModel(t *testing.T) {
	p, err := NewProvider(Config{APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.cfg.BaseURL != defaultBaseURL {
		t.Fatalf("BaseURL=%s", p.cfg.BaseURL)
	}
	if p.cfg.Model != defaultModel {
		t.Fatalf("Model=%s", p.cfg.Model)
	}
}

func TestNewProvider_StripsTrailingSlash(t *testing.T) {
	p, err := NewProvider(Config{APIKey: "sk-test", BaseURL: "https://api.deepseek.com/v1/"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.cfg.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("BaseURL=%s", p.cfg.BaseURL)
	}
}

func TestChat_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-1",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Looks fine."}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}
		}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	resp, err := p.Chat(context.Background(), ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Looks fine." {
		t.Fatalf("Content=%q", resp.Content)
	}
	if resp.Usage.TokensIn != 100 {
		t.Fatalf("TokensIn=%d", resp.Usage.TokensIn)
	}
	if resp.Usage.TokensOut != 20 {
		t.Fatalf("TokensOut=%d", resp.Usage.TokensOut)
	}
	if resp.FinishReason != FinishStop {
		t.Fatalf("FinishReason=%s", resp.FinishReason)
	}
}

func TestChat_Unauthorized_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Incorrect API key", "type": "invalid_request_error", "code": "invalid_api_key"}}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err not LLMError: %v", err)
	}
	if le.Kind != KindProvider4xx {
		t.Fatalf("Kind=%s", le.Kind)
	}
}

func TestChat_RateLimited_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error": {"message": "Rate limit reached", "type": "rate_limit_error", "code": "rate_limit_reached"}}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err not LLMError: %v", err)
	}
	if le.Kind != KindRateLimited {
		t.Fatalf("Kind=%s", le.Kind)
	}
	if le.RetryAfter.Seconds() != 30 {
		t.Fatalf("RetryAfter=%v", le.RetryAfter)
	}
}

func TestChat_ContextTooLong_400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"message": "context_length_exceeded", "type": "invalid_request_error", "code": "context_length_exceeded"}}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err not LLMError: %v", err)
	}
	if le.Kind != KindContextTooLong {
		t.Fatalf("Kind=%s", le.Kind)
	}
}

func TestChat_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"message": "internal", "type": "server_error"}}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err not LLMError: %v", err)
	}
	if le.Kind != KindProvider5xx {
		t.Fatalf("Kind=%s", le.Kind)
	}
}

func TestChat_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id": "x"`)) // truncated
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	_, err := p.Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}})
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err not LLMError: %v", err)
	}
	if le.Kind != KindUnknown {
		t.Fatalf("Kind=%s", le.Kind)
	}
}

func TestChat_DefaultModelFromConfig(t *testing.T) {
	var seenModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		seenModel = body.Model
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "x",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL, Model: "config-model"})
	_, err := p.Chat(context.Background(), ChatRequest{
		// no Model on request → falls back to config
		Messages: []Message{{Role: RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if seenModel != "config-model" {
		t.Fatalf("seenModel=%q want config-model", seenModel)
	}
}

func TestChat_RequestBodyShape(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	temp := 0.3
	maxTokens := 50
	req := ChatRequest{
		Model:       "m",
		Messages:    []Message{{Role: RoleSystem, Content: "be terse"}, {Role: RoleUser, Content: "hi"}},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	}
	if _, err := p.Chat(context.Background(), req); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// Validate fields present.
	if gotBody["model"] != "m" {
		t.Fatalf("model not set")
	}
	if _, ok := gotBody["messages"]; !ok {
		t.Fatalf("messages missing")
	}
	if _, ok := gotBody["stream"]; !ok {
		t.Fatalf("stream field should always be set to false by Chat")
	}
}

// helper used by stream_test.go too.
func newOKProvider(t *testing.T, h http.Handler) (*OpenAICompatibleProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	p, err := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	if err != nil {
		srv.Close()
		t.Fatalf("NewProvider: %v", err)
	}
	return p, srv
}
