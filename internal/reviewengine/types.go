// Package reviewengine implements the review pipeline for ReviewPilot.
//
// It satisfies the requirements defined in
// `specs/review-engine/spec.md` (6 Requirement): pipeline ordering,
// dedup, feedback learning, failure isolation, and observability.
//
// The package is platform-agnostic (no GitHub calls) and LLM-agnostic
// (consumes llmopenai.LLMProvider). Phase 4 will provide a
// `GitHubAdapter` that implements PlatformAdapter; phase 5 will provide
// a SQLite-backed FeedbackStore.
package reviewengine

import "time"

// Status enumerates the high-level outcome of a review run.
type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
)

// String returns the canonical status name.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether the status is a final outcome.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusDone, StatusFailed, StatusSkipped:
		return true
	}
	return false
}

// Request is the input to Engine.Run.
type Request struct {
	RepoID    string // opaque repo identifier (e.g., "bbbbbmy/reviewpilot")
	PRNumber  int
	CommitSHA string
	Diff      string // unified diff payload
	// SystemPrompt, when non-empty, overrides the default prompt.
	SystemPrompt string
}

// Comment is a single inline comment produced by the engine.
type Comment struct {
	FilePath          string `json:"file_path"`
	LineNumber        int    `json:"line_number"`
	Body              string `json:"body"`
	Fingerprint       string `json:"fingerprint"`
	PostedToPlatform  bool   `json:"posted_to_platform"`
	PlatformCommentID string `json:"platform_comment_id,omitempty"`
}

// ExistingComment is a comment already on the PR (used for dedup).
type ExistingComment struct {
	FilePath    string `json:"file_path"`
	LineNumber  int    `json:"line_number"`
	Body        string `json:"body"`
	Fingerprint string `json:"fingerprint"`
}

// Rating is the user-feedback rating for a comment.
type Rating string

const (
	RatingUp   Rating = "up"
	RatingDown Rating = "down"
)

// Feedback is one user feedback item tied to a (repo, comment) pair.
type Feedback struct {
	Rating    Rating    `json:"rating"`
	Text      string    `json:"text,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// StepName enumerates the 5 pipeline steps. Used as metric labels and
// for StepDurations map keys.
type StepName string

const (
	StepSplit    StepName = "split"
	StepContext  StepName = "context"
	StepLLM      StepName = "llm"
	StepDedup    StepName = "dedup"
	StepFeedback StepName = "feedback"
)

// Result is the structured outcome of a Run call.
type Result struct {
	Status        Status                     `json:"status"`
	Comments      []Comment                  `json:"comments,omitempty"`
	TokenUsage    TokenUsage                 `json:"token_usage"`
	StepDurations map[StepName]time.Duration `json:"step_durations,omitempty"`
	Reason        string                     `json:"reason,omitempty"`
	// Err surfaces a fatal error that the upper layer (HTTP/CLI) may
	// want to log. For Skipped / Done results, Err is nil.
	Err error `json:"-"`
}

// TokenUsage aggregates input/output tokens across the run.
type TokenUsage struct {
	TokensIn  int `json:"tokens_in"`
	TokensOut int `json:"tokens_out"`
}

// Add accumulates other into t (in place).
func (t *TokenUsage) Add(other TokenUsage) {
	t.TokensIn += other.TokensIn
	t.TokensOut += other.TokensOut
}
