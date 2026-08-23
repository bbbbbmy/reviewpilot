package webdashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bbbbbmy/reviewpilot/internal/reviewengine"
)

// ErrNotFound is returned when a lookup yields zero rows.
var ErrNotFound = errors.New("webdashboard: not found")

// SQLiteStore wraps a *sql.DB backed by modernc.org/sqlite. It is the
// canonical persistence layer for the web-dashboard.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path. Use ":memory:"
// for tests.
func Open(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable FK: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying DB.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// DB returns the underlying *sql.DB. Useful for tests that want to
// assert schema state directly.
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// --- Repo CRUD ---

// ListRepos returns all repos in lexical full_name order.
func (s *SQLiteStore) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, full_name, provider, model,
		max_input_tokens_per_pr, feedback_window_days, enabled, created_at
		FROM repo ORDER BY full_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Repo{}
	for rows.Next() {
		var r Repo
		if err := rows.Scan(&r.ID, &r.FullName, &r.Provider, &r.Model,
			&r.MaxInputTokensPerPR, &r.FeedbackWindowDays, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRepoByFullName returns the Repo matching fullName.
func (s *SQLiteStore) GetRepoByFullName(ctx context.Context, fullName string) (Repo, error) {
	var r Repo
	err := s.db.QueryRowContext(ctx, `SELECT id, full_name, provider, model,
		max_input_tokens_per_pr, feedback_window_days, enabled, created_at
		FROM repo WHERE full_name = ?`, fullName).Scan(
		&r.ID, &r.FullName, &r.Provider, &r.Model,
		&r.MaxInputTokensPerPR, &r.FeedbackWindowDays, &r.Enabled, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Repo{}, ErrNotFound
	}
	return r, err
}

// UpsertRepo inserts or updates a repo keyed by full_name. Returns
// the row id.
func (s *SQLiteStore) UpsertRepo(ctx context.Context, r Repo) (int64, error) {
	if r.FullName == "" {
		return 0, errors.New("UpsertRepo: FullName required")
	}
	if r.MaxInputTokensPerPR <= 0 {
		r.MaxInputTokensPerPR = 100_000
	}
	if r.FeedbackWindowDays <= 0 {
		r.FeedbackWindowDays = 30
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO repo (full_name, provider, model,
		max_input_tokens_per_pr, feedback_window_days, enabled, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(full_name) DO UPDATE SET
		  provider = excluded.provider,
		  model = excluded.model,
		  max_input_tokens_per_pr = excluded.max_input_tokens_per_pr,
		  feedback_window_days = excluded.feedback_window_days,
		  enabled = excluded.enabled`,
		r.FullName, r.Provider, r.Model, r.MaxInputTokensPerPR, r.FeedbackWindowDays, r.Enabled)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// UPDATE branch — fetch the id back.
		var gotID int64
		err := s.db.QueryRowContext(ctx, "SELECT id FROM repo WHERE full_name = ?", r.FullName).Scan(&gotID)
		if err != nil {
			return 0, err
		}
		id = gotID
	}
	return id, nil
}

// --- Review ---

// RecordReview persists a Review and returns its id.
func (s *SQLiteStore) RecordReview(ctx context.Context, r Review) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO review
		(repo_id, pr_number, commit_sha, status, reason, tokens_in, tokens_out,
		 started_at, finished_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RepoID, r.PRNumber, r.CommitSHA, string(r.Status), r.Reason,
		r.TokensIn, r.TokensOut, r.StartedAt, r.FinishedAt)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListReviews returns reviews filtered by repo id (0 = all) and
// status ("" = all), with limit/offset pagination.
func (s *SQLiteStore) ListReviews(ctx context.Context, repoID int64, status string, limit, offset int) ([]Review, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, repo_id, pr_number, commit_sha, status, reason,
		tokens_in, tokens_out, started_at, finished_at FROM review WHERE 1=1`
	args := []any{}
	if repoID > 0 {
		q += " AND repo_id = ?"
		args = append(args, repoID)
	}
	if status != "" {
		q += " AND status = ?"
		args = append(args, status)
	}
	q += " ORDER BY started_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Review{}
	for rows.Next() {
		var r Review
		var statusStr string
		var finishedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.RepoID, &r.PRNumber, &r.CommitSHA,
			&statusStr, &r.Reason, &r.TokensIn, &r.TokensOut, &r.StartedAt, &finishedAt); err != nil {
			return nil, err
		}
		r.Status = ReviewStatus(statusStr)
		if finishedAt.Valid {
			t := finishedAt.Time
			r.FinishedAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetReviewByID returns the Review matching id.
func (s *SQLiteStore) GetReviewByID(ctx context.Context, id int64) (Review, error) {
	var r Review
	var statusStr string
	var finishedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT id, repo_id, pr_number, commit_sha,
		status, reason, tokens_in, tokens_out, started_at, finished_at
		FROM review WHERE id = ?`, id).Scan(
		&r.ID, &r.RepoID, &r.PRNumber, &r.CommitSHA, &statusStr, &r.Reason,
		&r.TokensIn, &r.TokensOut, &r.StartedAt, &finishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Review{}, ErrNotFound
	}
	if err != nil {
		return Review{}, err
	}
	r.Status = ReviewStatus(statusStr)
	if finishedAt.Valid {
		t := finishedAt.Time
		r.FinishedAt = &t
	}
	return r, nil
}

// --- Comment + Feedback ---

// RecordComment persists a Comment.
func (s *SQLiteStore) RecordComment(ctx context.Context, c Comment) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO comment
		(review_id, file_path, line_number, body, fingerprint,
		 posted_to_platform, posted_to_platform_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ReviewID, c.FilePath, c.LineNumber, c.Body, c.Fingerprint,
		c.PostedToPlatform, c.PostedToPlatformAt)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// ListCommentsForReview returns all comments tied to reviewID.
func (s *SQLiteStore) ListCommentsForReview(ctx context.Context, reviewID int64) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, review_id, file_path, line_number,
		body, fingerprint, posted_to_platform, posted_to_platform_at
		FROM comment WHERE review_id = ? ORDER BY id ASC`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		var pat sql.NullTime
		if err := rows.Scan(&c.ID, &c.ReviewID, &c.FilePath, &c.LineNumber,
			&c.Body, &c.Fingerprint, &c.PostedToPlatform, &pat); err != nil {
			return nil, err
		}
		if pat.Valid {
			t := pat.Time
			c.PostedToPlatformAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddFeedback records one feedback entry.
func (s *SQLiteStore) AddFeedback(ctx context.Context, commentID int64, rating, text string) (Feedback, error) {
	if rating != RatingUp && rating != RatingDown {
		return Feedback{}, fmt.Errorf("invalid rating %q (must be %q or %q)", rating, RatingUp, RatingDown)
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO feedback
		(comment_id, rating, text, created_at)
		VALUES (?, ?, ?, ?)`,
		commentID, rating, text, now)
	if err != nil {
		return Feedback{}, err
	}
	id, _ := res.LastInsertId()
	return Feedback{ID: id, CommentID: commentID, Rating: rating, Text: text, CreatedAt: now}, nil
}

// --- FeedbackStore implementation (for reviewengine) ---

// RecentForRepo satisfies reviewengine.FeedbackStore.
func (s *SQLiteStore) RecentForRepo(ctx context.Context, repoFullName string, window time.Duration) ([]reviewengine.Feedback, error) {
	cutoff := time.Now().Add(-window)
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.rating, f.text, f.created_at
		FROM feedback f
		JOIN comment c ON c.id = f.comment_id
		JOIN review r ON r.id = c.review_id
		JOIN repo p ON p.id = r.repo_id
		WHERE p.full_name = ? AND f.created_at >= ?
		ORDER BY f.created_at ASC`, repoFullName, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []reviewengine.Feedback{}
	for rows.Next() {
		var f reviewengine.Feedback
		if err := rows.Scan(new(int64), &f.Rating, &f.Text, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Add satisfies reviewengine.FeedbackStore. RepoID is not used —
// the caller (server webhook handler) knows the full repo name; we
// store feedback keyed to the comment's repo via the JOIN chain above.
func (s *SQLiteStore) Add(_ context.Context, _ string, fb reviewengine.Feedback) error {
	// Not used in v0.4.0: feedback is added via AddFeedback(ctx, commentID, ...) on the dashboard side.
	return errors.New("webdashboard SQLiteStore.Add: use AddFeedback(ctx, commentID, rating, text)")
}

// Compile-time interface check.
var _ reviewengine.FeedbackStore = (*SQLiteStore)(nil)
