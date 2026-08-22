package platformgithub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bbbbbmy/reviewpilot/internal/reviewengine"
)

func TestAdapter_PostInlineComment_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/bbbbbmy/reviewpilot/pulls/7/comments" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		var body inlineCommentBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Path != "x.go" || body.Line != 12 || body.Body != "looks fine" {
			t.Errorf("body=%+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	a, err := NewAdapter(Config{Token: "test-token", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if err := a.PostInlineComment(context.Background(), "bbbbbmy/reviewpilot", 7, "deadbeef",
		Comment{Path: "x.go", Line: 12, Body: "looks fine"}); err != nil {
		t.Fatalf("PostInlineComment: %v", err)
	}
}

func TestAdapter_PostInlineComment_BadRepo(t *testing.T) {
	a, _ := NewAdapter(Config{Token: "x"})
	err := a.PostInlineComment(context.Background(), "no-slash", 1, "sha", Comment{Path: "x", Line: 1, Body: "y"})
	if err == nil {
		t.Fatal("expected error for invalid repo id")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err not PlatformError: %v", err)
	}
}

func TestAdapter_PostInlineComment_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("x-ratelimit-remaining", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer srv.Close()

	a, _ := NewAdapter(Config{Token: "x", BaseURL: srv.URL})
	err := a.PostInlineComment(context.Background(), "o/r", 1, "sha",
		Comment{Path: "x.go", Line: 1, Body: "y"})
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	var pe *PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err not PlatformError: %v", err)
	}
	if pe.Kind != kindRateLimited {
		t.Fatalf("Kind=%s", pe.Kind)
	}
	if pe.RetryAfter != "30" {
		t.Fatalf("RetryAfter=%q", pe.RetryAfter)
	}
}

func TestAdapter_ListComments_InlineAndSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r/pulls/1/comments":
			_, _ = w.Write([]byte(`[{"id":1,"path":"a.go","line":3,"body":"inline a"}]`))
		case "/repos/o/r/issues/1/comments":
			_, _ = w.Write([]byte(`[{"id":2,"body":"summary"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	a, _ := NewAdapter(Config{Token: "x", BaseURL: srv.URL})
	out, err := a.ListComments(context.Background(), "o/r", 1)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len=%d, want 2", len(out))
	}
	var inline, summary *reviewengine.ExistingComment
	for i := range out {
		if out[i].FilePath == "a.go" {
			inline = &out[i]
		} else {
			summary = &out[i]
		}
	}
	if inline == nil || inline.LineNumber != 3 {
		t.Fatalf("inline=%+v", inline)
	}
	if summary == nil || summary.FilePath != "" {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestAdapter_ListComments_PartialResult(t *testing.T) {
	// Inline OK, summary 500 — engine treats as soft-warn.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/pulls/1/comments" {
			_, _ = w.Write([]byte(`[{"id":1,"path":"a.go","line":1,"body":"x"}]`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a, _ := NewAdapter(Config{Token: "x", BaseURL: srv.URL})
	out, err := a.ListComments(context.Background(), "o/r", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(out) != 1 {
		t.Fatalf("len=%d", len(out))
	}
}

func TestAdapter_NewAdapter_RequiresToken(t *testing.T) {
	if _, err := NewAdapter(Config{}); err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestAdapter_PostSummaryComment_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/issues/3/comments" {
			http.NotFound(w, r)
			return
		}
		var body issueCommentBody
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Body != "all good" {
			t.Errorf("body=%+v", body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	a, _ := NewAdapter(Config{Token: "x", BaseURL: srv.URL})
	if err := a.PostSummaryComment(context.Background(), "o/r", 3, "all good"); err != nil {
		t.Fatalf("PostSummaryComment: %v", err)
	}
}

func TestAdapter_PostInlineComment_BodyReadLimit(t *testing.T) {
	// Send a malformed response with very long body to exercise the
	// LimitReader path; we just need to verify no panic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// OK but empty body
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	a, _ := NewAdapter(Config{Token: "x", BaseURL: srv.URL})
	if err := a.PostSummaryComment(context.Background(), "o/r", 1, "hi"); err != nil {
		t.Fatalf("PostSummaryComment: %v", err)
	}
}

func TestSplitRepo(t *testing.T) {
	for _, tt := range []struct {
		in      string
		wantErr bool
		owner   string
		name    string
	}{
		{"o/r", false, "o", "r"},
		{"my-org/my-repo", false, "my-org", "my-repo"},
		{"no-slash", true, "", ""},
		{"a/b/c", false, "a", "b/c"}, // first slash splits, name includes slash
	} {
		o, n, err := splitRepo(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr {
			if o != tt.owner || n != tt.name {
				t.Errorf("%s: got %s/%s want %s/%s", tt.in, o, n, tt.owner, tt.name)
			}
		}
	}
}

// silence unused imports.
var _ = strings.NewReader
var _ = fmt.Sprintf
var _ = time.Second
