package webdashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// --- HTML handlers ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/reviews", http.StatusFound)
}

func (s *Server) handleReposList(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	repos, err := s.store.ListRepos(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderTemplate(w, "repos_list.html", map[string]any{
		"Title": "Repositories",
		"Repos": repos,
	})
}

func (s *Server) handleReposCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	repo := Repo{
		FullName:            strings.TrimSpace(r.FormValue("full_name")),
		Provider:            strings.TrimSpace(r.FormValue("provider")),
		Model:               strings.TrimSpace(r.FormValue("model")),
		MaxInputTokensPerPR: atoiOr(r.FormValue("max_input_tokens_per_pr"), 100_000),
		FeedbackWindowDays:  atoiOr(r.FormValue("feedback_window_days"), 30),
		Enabled:             r.FormValue("enabled") == "1",
	}
	if repo.FullName == "" || !strings.Contains(repo.FullName, "/") {
		http.Error(w, "full_name must be owner/name", http.StatusBadRequest)
		return
	}
	if _, err := s.store.UpsertRepo(r.Context(), repo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/repos", http.StatusSeeOther)
}

func (s *Server) handleReposUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	existing, err := s.findRepoByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	existing.Enabled = r.FormValue("enabled") == "1"
	existing.MaxInputTokensPerPR = atoiOr(r.FormValue("max_input_tokens_per_pr"), existing.MaxInputTokensPerPR)
	existing.FeedbackWindowDays = atoiOr(r.FormValue("feedback_window_days"), existing.FeedbackWindowDays)
	if _, err := s.store.UpsertRepo(r.Context(), existing); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/repos", http.StatusSeeOther)
}

func (s *Server) handleReviewsList(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	repos, _ := s.store.ListRepos(r.Context())
	reviews, err := s.store.ListReviews(r.Context(),
		parseRepoFilter(r, repos),
		r.URL.Query().Get("status"),
		50, pageOffset(r, 50))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderTemplate(w, "reviews_list.html", map[string]any{
		"Title":   "Reviews",
		"Reviews": reviews,
		"Repos":   repos,
	})
}

func (s *Server) handleReviewDetail(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	review, err := s.store.GetReviewByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	comments, err := s.store.ListCommentsForReview(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.renderTemplate(w, "review_detail.html", map[string]any{
		"Title":    fmt.Sprintf("Review #%d", id),
		"Review":   review,
		"Comments": comments,
	})
}

func (s *Server) handleFeedbackSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil {
		http.Error(w, "bad comment id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rating := r.FormValue("rating")
	if rating != RatingUp && rating != RatingDown {
		http.Error(w, "rating must be 'up' or 'down'", http.StatusBadRequest)
		return
	}
	if _, err := s.store.AddFeedback(r.Context(), cid, rating, r.FormValue("text")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Redirect back to the review detail page.
	rid := r.PathValue("id")
	http.Redirect(w, r, "/reviews/"+rid, http.StatusSeeOther)
}

// --- API handlers (JSON) ---

func (s *Server) apiListRepos(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	repos, err := s.store.ListRepos(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func (s *Server) apiUpsertRepo(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	var repo Repo
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("decode: %w", err))
		return
	}
	if repo.FullName == "" || !strings.Contains(repo.FullName, "/") {
		writeJSONError(w, http.StatusBadRequest, errors.New("full_name required"))
		return
	}
	id, err := s.store.UpsertRepo(r.Context(), repo)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "ok": true})
}

func (s *Server) apiListReviews(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	repos, _ := s.store.ListRepos(r.Context())
	reviews, err := s.store.ListReviews(r.Context(),
		parseRepoFilter(r, repos),
		r.URL.Query().Get("status"),
		50, pageOffset(r, 50))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": reviews})
}

func (s *Server) apiGetReview(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, errors.New("bad id"))
		return
	}
	review, err := s.store.GetReviewByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, err)
			return
		}
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	comments, err := s.store.ListCommentsForReview(r.Context(), id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"review": review, "comments": comments})
}

func (s *Server) apiSubmitFeedback(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	cid, err := strconv.ParseInt(r.PathValue("cid"), 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, errors.New("bad comment id"))
		return
	}
	var body struct {
		Rating string `json:"rating"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("decode: %w", err))
		return
	}
	if body.Rating != RatingUp && body.Rating != RatingDown {
		writeJSONError(w, http.StatusBadRequest, errors.New("rating must be 'up' or 'down'"))
		return
	}
	fb, err := s.store.AddFeedback(r.Context(), cid, body.Rating, body.Text)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, fb)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func pageOffset(r *http.Request, defaultSize int) int {
	page := atoiOr(r.URL.Query().Get("page"), 1)
	if page < 1 {
		page = 1
	}
	return (page - 1) * defaultSize
}

func parseRepoFilter(r *http.Request, repos []Repo) int64 {
	fullName := r.URL.Query().Get("repo")
	if fullName == "" {
		return 0
	}
	for _, rp := range repos {
		if rp.FullName == fullName {
			return rp.ID
		}
	}
	return 0
}

// findRepoByID iterates ListRepos to find an id. Used by the update
// handler to get the existing row's full_name (which is required
// for UpsertRepo's ON CONFLICT clause).
func (s *Server) findRepoByID(ctx context.Context, id int64) (Repo, error) {
	if s.store == nil {
		return Repo{}, errors.New("no store")
	}
	repos, err := s.store.ListRepos(ctx)
	if err != nil {
		return Repo{}, err
	}
	for _, r := range repos {
		if r.ID == id {
			return r, nil
		}
	}
	return Repo{}, ErrNotFound
}
