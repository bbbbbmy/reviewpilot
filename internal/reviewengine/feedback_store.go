package reviewengine

import (
	"context"
	"sort"
	"sync"
	"time"
)

// FeedbackStore persists per-repo feedback so the engine can inject
// it into the next review's prompt. Phase 5 will provide a SQLite
// implementation; the in-memory default is enough for tests and v0.0.x
// single-process deployments.
type FeedbackStore interface {
	RecentForRepo(ctx context.Context, repoID string, window time.Duration) ([]Feedback, error)
	Add(ctx context.Context, repoID string, fb Feedback) error
}

// InMemoryFeedbackStore is a thread-safe FeedbackStore suitable for
// tests and single-process servers. It bounds memory at 10,000 entries
// per repo and trims oldest-first when full.
type InMemoryFeedbackStore struct {
	mu         sync.Mutex
	entries    map[string][]feedbackEntry
	maxPerRepo int
}

type feedbackEntry struct {
	fb        Feedback
	createdAt time.Time
}

// NewInMemoryFeedbackStore returns an empty store with default
// 10k/repo cap.
func NewInMemoryFeedbackStore() *InMemoryFeedbackStore {
	return &InMemoryFeedbackStore{
		entries:    map[string][]feedbackEntry{},
		maxPerRepo: 10_000,
	}
}

// Add inserts a feedback entry, trimming oldest when over the cap.
func (s *InMemoryFeedbackStore) Add(_ context.Context, repoID string, fb Feedback) error {
	if fb.CreatedAt.IsZero() {
		fb.CreatedAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	es := s.entries[repoID]
	es = append(es, feedbackEntry{fb: fb, createdAt: fb.CreatedAt})
	if len(es) > s.maxPerRepo {
		es = es[len(es)-s.maxPerRepo:]
	}
	s.entries[repoID] = es
	return nil
}

// RecentForRepo returns entries for repoID within the time window,
// sorted by createdAt ascending.
func (s *InMemoryFeedbackStore) RecentForRepo(_ context.Context, repoID string, window time.Duration) ([]Feedback, error) {
	cutoff := time.Now().Add(-window)
	s.mu.Lock()
	defer s.mu.Unlock()
	es := s.entries[repoID]
	out := make([]Feedback, 0, len(es))
	for _, e := range es {
		if e.createdAt.After(cutoff) {
			out = append(out, e.fb)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// Size returns the number of entries for a given repo (test helper).
func (s *InMemoryFeedbackStore) Size(repoID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries[repoID])
}
