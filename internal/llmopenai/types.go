// Package llmopenai implements the OpenAI-compatible LLM provider for
// ReviewPilot. It satisfies the LLMProvider interface defined in
// `specs/llm-providers`.
package llmopenai

import "context"

// Role enumerates the four supported chat-completion message roles.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one chat-completion message.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the input to a chat-completion call.
//
// Fields marked * are optional; zero values follow the upstream defaults.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	// Stream, when true, switches Chat to a streaming call. Most callers
	// prefer ChatStream instead.
	Stream *bool `json:"stream,omitempty"`
}

// FinishReason is the reason a model stopped generating.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishError         FinishReason = "error"
)

// Usage counts tokens consumed on the upstream.
type Usage struct {
	TokensIn  int `json:"prompt_tokens"`
	TokensOut int `json:"completion_tokens"`
}

// ChatResponse is the non-streaming output.
type ChatResponse struct {
	ID           string       `json:"id"`
	Content      string       `json:"content"`
	Usage        Usage        `json:"usage"`
	FinishReason FinishReason `json:"finish_reason"`
}

// LLMProvider is the interface consumed by review-engine.
//
// Implementations must be safe for concurrent use.
type LLMProvider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
