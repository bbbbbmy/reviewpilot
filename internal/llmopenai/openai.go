package llmopenai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4o-mini"
	defaultTimeout = 30 * time.Second
	opChat         = "openai.chat"
	opStream       = "openai.stream"
	maxBodyBytes   = 10 << 20 // 10 MiB cap on response bodies
)

// Config wires up an OpenAICompatibleProvider. Zero-valued or empty
// fields fall back to safe defaults.
type Config struct {
	// BaseURL is the upstream root (no trailing slash). Default:
	// "https://api.openai.com/v1".
	BaseURL string
	// APIKey is sent as `Authorization: Bearer <key>`. Required.
	APIKey string
	// Model is the model name passed on every request. Default:
	// "gpt-4o-mini".
	Model string
	// HTTPClient allows callers to inject a custom transport (timeouts,
	// retries, instrumentation). Default: &http.Client{Timeout: 30s}.
	HTTPClient *http.Client
}

// OpenAICompatibleProvider is an LLMProvider that talks to OpenAI's
// chat-completions endpoint, and any vendor implementing the same wire
// protocol (DeepSeek, Moonshot, Zhipu, …).
type OpenAICompatibleProvider struct {
	cfg    Config
	client *http.Client
}

// NewProvider returns an OpenAICompatibleProvider. An empty APIKey is
// rejected so misconfiguration fails fast at startup.
func NewProvider(cfg Config) (*OpenAICompatibleProvider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("llmopenai: APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultTimeout}
	}
	return &OpenAICompatibleProvider{cfg: cfg, client: cfg.HTTPClient}, nil
}

// Chat satisfies LLMProvider. Stream fields on the request are
// ignored — call ChatStream for streaming.
func (p *OpenAICompatibleProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	req.Model = p.modelOrDefault(req.Model)
	req.Stream = boolPtr(false)

	httpReq, err := p.buildRequest(ctx, "/chat/completions", req, false)
	if err != nil {
		return ChatResponse{}, err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return ChatResponse{}, &LLMError{Op: opChat, Kind: KindUnknown, Message: err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody openAIErrorBody
		_ = json.Unmarshal(body, &errBody)
		return ChatResponse{}, classifyHTTPError(opChat, resp.StatusCode, resp.Header, errBody)
	}

	var raw struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason FinishReason `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return ChatResponse{}, &LLMError{
			Op:      opChat,
			Kind:    KindUnknown,
			Message: fmt.Sprintf("decode response: %v", err),
		}
	}

	if len(raw.Choices) == 0 {
		return ChatResponse{}, &LLMError{
			Op:      opChat,
			Kind:    KindUnknown,
			Message: "no choices in response",
		}
	}

	return ChatResponse{
		ID:           raw.ID,
		Content:      raw.Choices[0].Message.Content,
		Usage:        raw.Usage,
		FinishReason: raw.Choices[0].FinishReason,
	}, nil
}

// ChatStream performs a streaming chat-completion and invokes onDelta
// for each content chunk the upstream emits. The final accumulated
// usage is returned via the second return value when the upstream
// emits it on the terminal chunk.
//
// Returns nil on a clean `[DONE]` termination.
func (p *OpenAICompatibleProvider) ChatStream(ctx context.Context, req ChatRequest, onDelta func(string)) error {
	req.Model = p.modelOrDefault(req.Model)
	req.Stream = boolPtr(true)

	httpReq, err := p.buildRequest(ctx, "/chat/completions", req, true)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return &LLMError{Op: opStream, Kind: KindUnknown, Message: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		var errBody openAIErrorBody
		_ = json.Unmarshal(body, &errBody)
		return classifyHTTPError(opStream, resp.StatusCode, resp.Header, errBody)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Tokens can be > 64 KiB; lift the buffer ceiling to 1 MiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for scanner.Scan() {
		// Drop on ctx cancellation between lines.
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Tolerate partial / malformed chunks silently; the upstream
			// may interleave heartbeats or non-JSON SSE comments.
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" && onDelta != nil {
			onDelta(chunk.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return &LLMError{Op: opStream, Kind: KindUnknown, Message: err.Error()}
	}
	return nil
}

func (p *OpenAICompatibleProvider) buildRequest(ctx context.Context, path string, body any, stream bool) (*http.Request, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, &LLMError{Op: opChat, Kind: KindUnknown, Message: fmt.Sprintf("encode request: %v", err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, &LLMError{Op: opChat, Kind: KindUnknown, Message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

func (p *OpenAICompatibleProvider) modelOrDefault(model string) string {
	if model != "" {
		return model
	}
	return p.cfg.Model
}

func boolPtr(b bool) *bool { return &b }
