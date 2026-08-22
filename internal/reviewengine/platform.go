package reviewengine

import "context"

// PlatformAdapter is the platform integration surface used by the
// engine for dedup. Phase 4 (add-reviewpilot-impl-phase4) provides the
// real `GitHubAdapter` implementation.
//
// Implementations must be safe for concurrent use.
type PlatformAdapter interface {
	// ListComments returns all existing comments for a PR, used by
	// dedup. The slice must include each comment's Fingerprint where
	// the platform exposes it; the engine computes its own when empty.
	ListComments(ctx context.Context, repoID string, prNumber int) ([]ExistingComment, error)
}
