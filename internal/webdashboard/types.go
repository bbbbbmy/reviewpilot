package webdashboard

import "time"

// Repo represents a connected GitHub repository.
type Repo struct {
	ID                  int64     `json:"id"`
	FullName            string    `json:"full_name"` // "owner/name"
	Provider            string    `json:"provider"`  // "openai-default"
	Model               string    `json:"model"`
	MaxInputTokensPerPR int       `json:"max_input_tokens_per_pr"`
	FeedbackWindowDays  int       `json:"feedback_window_days"`
	Enabled             bool      `json:"enabled"`
	CreatedAt           time.Time `json:"created_at"`
}

// ReviewStatus mirrors reviewengine.Status string values.
type ReviewStatus string

const (
	StatusQueued  ReviewStatus = "queued"
	StatusRunning ReviewStatus = "running"
	StatusDone    ReviewStatus = "done"
	StatusFailed  ReviewStatus = "failed"
	StatusSkipped ReviewStatus = "skipped"
)

// Review is one PR-level review run.
type Review struct {
	ID         int64        `json:"id"`
	RepoID     int64        `json:"repo_id"`
	PRNumber   int          `json:"pr_number"`
	CommitSHA  string       `json:"commit_sha"`
	Status     ReviewStatus `json:"status"`
	Reason     string       `json:"reason,omitempty"`
	TokensIn   int          `json:"tokens_in"`
	TokensOut  int          `json:"tokens_out"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
}

// Comment is one inline or summary comment produced by the engine.
type Comment struct {
	ID                 int64      `json:"id"`
	ReviewID           int64      `json:"review_id"`
	FilePath           string     `json:"file_path"`
	LineNumber         int        `json:"line_number"`
	Body               string     `json:"body"`
	Fingerprint        string     `json:"fingerprint"`
	PostedToPlatform   bool       `json:"posted_to_platform"`
	PostedToPlatformAt *time.Time `json:"posted_to_platform_at,omitempty"`
}

// Feedback rating constants mirror reviewengine.Rating.
const (
	RatingUp   = "up"
	RatingDown = "down"
)

// Feedback is one user feedback item tied to a comment.
type Feedback struct {
	ID        int64     `json:"id"`
	CommentID int64     `json:"comment_id"`
	Rating    string    `json:"rating"`
	Text      string    `json:"text,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
