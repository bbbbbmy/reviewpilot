package llmopenai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStream_OK_DeltasAccumulate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("no flusher")
			return
		}
		// Three streamed tokens + DONE.
		chunks := []string{
			`{"choices":[{"index":0,"delta":{"content":"hello "}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"world"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"!"}}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})

	var collected []string
	err := p.ChatStream(context.Background(),
		ChatRequest{
			Model:    "m",
			Messages: []Message{{Role: RoleUser, Content: "hi"}},
		},
		func(s string) { collected = append(collected, s) },
	)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if got := strings.Join(collected, ""); got != "hello world!" {
		t.Fatalf("streamed text=%q", got)
	}
}

func TestChatStream_5xx_ReturnsLLMError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"bad gateway","type":"server_error"}}`))
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	err := p.ChatStream(context.Background(),
		ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		func(string) {},
	)
	var le *LLMError
	if !errors.As(err, &le) {
		t.Fatalf("err not LLMError: %v", err)
	}
	if le.Kind != KindProvider5xx {
		t.Fatalf("Kind=%s", le.Kind)
	}
}

func TestChatStream_AcceptsSSEResponse(t *testing.T) {
	// Verify request actually carries Accept: text/event-stream header.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Errorf("Accept=%q want text/event-stream", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	if err := p.ChatStream(context.Background(),
		ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}},
		nil,
	); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
}

func TestChatStream_SkipsSSEComments(t *testing.T) {
	// SSE comment lines start with ':' and should be ignored.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, ": keep-alive\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p, _ := NewProvider(Config{APIKey: "sk-test", BaseURL: srv.URL})
	var got []string
	if err := p.ChatStream(context.Background(),
		ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}},
		func(s string) { got = append(got, s) },
	); err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if strings.Join(got, "") != "ok" {
		t.Fatalf("got=%q want ok", strings.Join(got, ""))
	}
}
