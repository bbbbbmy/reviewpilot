package reviewengine

import (
	"context"
	"log/slog"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
)

// Engine is the entry-point interface for the review pipeline.
// Implementations must be safe for concurrent use.
type Engine interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// EngineDeps wires the engine's external collaborators.
type EngineDeps struct {
	LLM      llmopenai.LLMProvider
	Platform PlatformAdapter
	Feedback FeedbackStore
	Metrics  *Metrics
	// MaxInputTokensPerPR bounds the per-PR input budget; passed to
	// Split. Defaults to 100_000 when zero.
	MaxInputTokensPerPR int
	// FeedbackWindow is how far back to query feedback when building
	// the next prompt. Defaults to 30 days when zero.
	FeedbackWindowDays int
	// Logger is the slog.Logger for structured logging. Defaults to
	// slog.Default() when nil.
	Logger *slog.Logger
}

// DefaultEngine is the production Engine implementation.
type DefaultEngine struct {
	deps   EngineDeps
	logger *slog.Logger
	budget int
	window int
}

// NewDefaultEngine validates dependencies and returns a DefaultEngine.
// LLM is required. Platform may be nil for CLI mode (no platform
// dedup); server mode must supply a real PlatformAdapter.
func NewDefaultEngine(deps EngineDeps) (*DefaultEngine, error) {
	if deps.LLM == nil {
		return nil, ErrMissingLLM
	}
	// Platform is optional — CLI mode passes nil.
	if deps.Feedback == nil {
		deps.Feedback = NewInMemoryFeedbackStore()
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	budget := deps.MaxInputTokensPerPR
	if budget <= 0 {
		budget = 100_000
	}
	windowDays := deps.FeedbackWindowDays
	if windowDays <= 0 {
		windowDays = 30
	}
	return &DefaultEngine{
		deps:   deps,
		logger: logger,
		budget: budget,
		window: windowDays,
	}, nil
}

// Run implements Engine. The orchestration of the 5 steps is
// delegated to Pipeline.Run; DefaultEngine exists primarily to apply
// top-level logging, metrics, and status emission.
func (e *DefaultEngine) Run(ctx context.Context, req Request) (Result, error) {
	p := NewPipeline(PipelineDeps{
		LLM:      e.deps.LLM,
		Platform: e.deps.Platform,
		Feedback: e.deps.Feedback,
		Metrics:  e.deps.Metrics,
		Logger:   e.logger,
		Budget:   e.budget,
		Window:   e.window,
	})
	return p.Run(ctx, req)
}
