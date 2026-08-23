package webdashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bbbbbmy/reviewpilot/internal/reviewengine"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpen_InMemory(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Fatal("DB nil")
	}
}

func TestStore_RepoCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, err := s.UpsertRepo(ctx, Repo{FullName: "bbbbbmy/reviewpilot", Provider: "openai-default", Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if id == 0 {
		t.Fatal("id=0")
	}

	// List
	repos, err := s.ListRepos(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("len=%d", len(repos))
	}
	if repos[0].FullName != "bbbbbmy/reviewpilot" {
		t.Fatalf("FullName=%s", repos[0].FullName)
	}

	// Get by full name
	got, err := s.GetRepoByFullName(ctx, "bbbbbmy/reviewpilot")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id {
		t.Fatalf("ID=%d want %d", got.ID, id)
	}

	// Upsert (update)
	_, err = s.UpsertRepo(ctx, Repo{FullName: "bbbbbmy/reviewpilot", Provider: "openai-default", Model: "gpt-4o", MaxInputTokensPerPR: 50000})
	if err != nil {
		t.Fatalf("Upsert update: %v", err)
	}
	repos, _ = s.ListRepos(ctx)
	if len(repos) != 1 || repos[0].Model != "gpt-4o" {
		t.Fatalf("after upsert: %+v", repos)
	}
	if repos[0].MaxInputTokensPerPR != 50000 {
		t.Fatalf("MaxInputTokensPerPR=%d", repos[0].MaxInputTokensPerPR)
	}

	// NotFound
	_, err = s.GetRepoByFullName(ctx, "ghost/repo")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestStore_Reviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	repoID, _ := s.UpsertRepo(ctx, Repo{FullName: "o/r", Provider: "p", Model: "m"})

	rid, err := s.RecordReview(ctx, Review{
		RepoID:    repoID,
		PRNumber:  1,
		CommitSHA: "deadbeef",
		Status:    StatusDone,
		TokensIn:  100,
		TokensOut: 50,
		StartedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("RecordReview: %v", err)
	}
	if rid == 0 {
		t.Fatal("id=0")
	}

	list, err := s.ListReviews(ctx, repoID, "", 50, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len=%d", len(list))
	}

	// With status filter
	_, err = s.ListReviews(ctx, repoID, "skipped", 50, 0)
	if err != nil {
		t.Fatalf("List status: %v", err)
	}

	// GetReviewByID
	got, err := s.GetReviewByID(ctx, rid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PRNumber != 1 || got.TokensIn != 100 {
		t.Fatalf("got=%+v", got)
	}

	// NotFound
	_, err = s.GetReviewByID(ctx, 99999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestStore_CommentsAndFeedback(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	repoID, _ := s.UpsertRepo(ctx, Repo{FullName: "o/r", Provider: "p", Model: "m"})
	rid, _ := s.RecordReview(ctx, Review{RepoID: repoID, PRNumber: 1, Status: StatusDone})

	cid, err := s.RecordComment(ctx, Comment{
		ReviewID:    rid,
		FilePath:    "x.go",
		LineNumber:  12,
		Body:        "looks fine",
		Fingerprint: "abc123",
	})
	if err != nil {
		t.Fatalf("RecordComment: %v", err)
	}
	if cid == 0 {
		t.Fatal("comment id=0")
	}

	// List comments
	comments, err := s.ListCommentsForReview(ctx, rid)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("len=%d", len(comments))
	}

	// AddFeedback
	fb, err := s.AddFeedback(ctx, cid, RatingUp, "great")
	if err != nil {
		t.Fatalf("AddFeedback: %v", err)
	}
	if fb.Rating != RatingUp {
		t.Fatalf("rating=%s", fb.Rating)
	}

	// Invalid rating
	_, err = s.AddFeedback(ctx, cid, "?", "?")
	if err == nil {
		t.Fatal("expected invalid rating error")
	}
}

func TestStore_FeedbackStore_RecentForRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	repoID, _ := s.UpsertRepo(ctx, Repo{FullName: "bbbbbmy/r", Provider: "p", Model: "m"})
	rid, _ := s.RecordReview(ctx, Review{RepoID: repoID, PRNumber: 1, Status: StatusDone})
	cid, _ := s.RecordComment(ctx, Comment{ReviewID: rid, Body: "x", Fingerprint: "fp"})
	_, _ = s.AddFeedback(ctx, cid, RatingUp, "👍")

	got, err := s.RecentForRepo(ctx, "bbbbbmy/r", 24*time.Hour)
	if err != nil {
		t.Fatalf("RecentForRepo: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Rating != RatingUp {
		t.Fatalf("rating=%s", got[0].Rating)
	}
}

func TestStore_FeedbackStore_Add_Unsupported(t *testing.T) {
	s := newTestStore(t)
	err := s.Add(context.Background(), "x", reviewengine.Feedback{Rating: reviewengine.RatingUp})
	if err == nil {
		t.Fatal("expected Add to error (server uses AddFeedback instead)")
	}
}
