package webdashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s, err := NewServerWithStore(store)
	if err != nil {
		t.Fatalf("NewServerWithStore: %v", err)
	}
	return s
}

func TestServer_Healthz_OK(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServer_IndexRedirects(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/reviews" {
		t.Fatalf("Location=%s", loc)
	}
}

func TestServer_ReposList_GET(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/repos", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<h2>Add repository</h2>") {
		t.Fatalf("body missing Add repo form: %q", rec.Body.String())
	}
}

func TestServer_ReposCreate_POST(t *testing.T) {
	s := newTestServer(t)
	form := strings.NewReader("full_name=bbbbbmy/reviewpilot&provider=p&model=m")
	req := httptest.NewRequest(http.MethodPost, "/repos", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// Verify it landed
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/repos", nil)
	s.Handler().ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "bbbbbmy/reviewpilot") {
		t.Fatalf("repo not in list: %s", rec2.Body.String())
	}
}

func TestServer_ReposCreate_InvalidName(t *testing.T) {
	s := newTestServer(t)
	form := strings.NewReader("full_name=invalid&provider=p&model=m")
	req := httptest.NewRequest(http.MethodPost, "/repos", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServer_ReviewsList_GET_Empty(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No reviews yet") {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestServer_ReviewsList_GET_WithData(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	repoID, _ := s.store.UpsertRepo(ctx, Repo{FullName: "bbbbbmy/r", Provider: "p", Model: "m"})
	rid, _ := s.store.RecordReview(ctx, Review{
		RepoID: repoID, PRNumber: 1, Status: StatusDone, TokensIn: 100, TokensOut: 50,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reviews", nil)
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "/reviews/"+itoa(int64ToA(rid))) {
		// iota path is a bit ugly; just check the review id appears.
		t.Fatalf("body missing review link: %s", rec.Body.String())
	}
}

func TestServer_ReviewDetail_GET(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	repoID, _ := s.store.UpsertRepo(ctx, Repo{FullName: "bbbbbmy/r", Provider: "p", Model: "m"})
	rid, _ := s.store.RecordReview(ctx, Review{RepoID: repoID, PRNumber: 1, Status: StatusDone})
	_, _ = s.store.RecordComment(ctx, Comment{ReviewID: rid, Body: "comment body", Fingerprint: "abc"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reviews/1", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "comment body") {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestServer_ReviewDetail_NotFound(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reviews/99999", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServer_FeedbackSubmit_OK(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	repoID, _ := s.store.UpsertRepo(ctx, Repo{FullName: "bbbbbmy/r", Provider: "p", Model: "m"})
	rid, _ := s.store.RecordReview(ctx, Review{RepoID: repoID, PRNumber: 1, Status: StatusDone})
	_, _ = s.store.RecordComment(ctx, Comment{ReviewID: rid, Body: "x", Fingerprint: "fp"})

	form := strings.NewReader("rating=up&text=great")
	req := httptest.NewRequest(http.MethodPost, "/reviews/1/comments/1/feedback", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServer_FeedbackSubmit_BadRating(t *testing.T) {
	s := newTestServer(t)
	form := strings.NewReader("rating=meh")
	req := httptest.NewRequest(http.MethodPost, "/reviews/1/comments/1/feedback", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

// --- API ---

func TestServer_API_ListRepos(t *testing.T) {
	s := newTestServer(t)
	_, _ = s.store.UpsertRepo(context.Background(), Repo{FullName: "o/r", Provider: "p", Model: "m"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body struct {
		Repos []Repo `json:"repos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Repos) != 1 {
		t.Fatalf("len=%d", len(body.Repos))
	}
}

func TestServer_API_UpsertRepo(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(Repo{FullName: "bbbbbmy/reviewpilot", Provider: "openai", Model: "gpt-4o-mini"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == 0 {
		t.Fatal("id=0")
	}
}

func TestServer_API_ListReviews(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServer_API_GetReview(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	repoID, _ := s.store.UpsertRepo(ctx, Repo{FullName: "o/r", Provider: "p", Model: "m"})
	rid, _ := s.store.RecordReview(ctx, Review{RepoID: repoID, PRNumber: 1, Status: StatusDone})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/1", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if int64FromAny(body["review"].(map[string]any)["id"]) != rid {
		t.Fatalf("review id mismatch: %v", body)
	}
}

func TestServer_API_GetReview_NotFound(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/99999", nil)
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServer_API_SubmitFeedback(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	repoID, _ := s.store.UpsertRepo(ctx, Repo{FullName: "o/r", Provider: "p", Model: "m"})
	rid, _ := s.store.RecordReview(ctx, Review{RepoID: repoID, PRNumber: 1, Status: StatusDone})
	_, _ = s.store.RecordComment(ctx, Comment{ReviewID: rid, Body: "x", Fingerprint: "fp"})

	body, _ := json.Marshal(map[string]string{"rating": "down", "text": "not helpful"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/1/comments/1/feedback", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// --- helpers ---

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func int64ToA(n int64) int64 { return n }
func int64FromAny(v any) int64 {
	if n, ok := v.(float64); ok {
		return int64(n)
	}
	return 0
}
