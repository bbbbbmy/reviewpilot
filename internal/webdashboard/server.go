// Package webdashboard exposes the HTTP server for ReviewPilot's Web UI
// and JSON API.
//
// In v0.4.0 the package implements the full web-dashboard capability
// spec: config CRUD, review records, feedback submission, basic-auth,
// and a versioned /api/v1/* surface. Server.Handler() returns an
// authenticated mux (except for /healthz and /metrics, which are
// intentionally exposed without auth for monitoring).
package webdashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server holds the HTTP server state and dependencies.
type Server struct {
	store     *SQLiteStore
	templates *template.Template
}

// NewServer returns a Server without a SQLite backing — useful for
// backward compatibility with cmd/reviewpilot/main.go before v0.4.0.
// New callers should use NewServerWithStore.
func NewServer() *Server { return &Server{} }

// NewServerWithStore returns a fully-wired Server. It parses HTML
// templates at startup; a template error is returned to the caller.
func NewServerWithStore(store *SQLiteStore) (*Server, error) {
	funcs := template.FuncMap{
		"safeJS": func(s string) template.JS { return template.JS(s) },
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
	}
	tpl, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{store: store, templates: tpl}, nil
}

// Handler returns the http.Handler that serves the API and HTML.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public endpoints (no auth).
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /metrics", s.handleMetrics) // optional; caller wires Prometheus

	// Authenticated endpoints (Basic Auth applied below).
	authed := http.NewServeMux()
	authed.HandleFunc("GET /{$}", s.handleIndex)
	authed.HandleFunc("GET /repos", s.handleReposList)
	authed.HandleFunc("POST /repos", s.handleReposCreate)
	authed.HandleFunc("POST /repos/{id}", s.handleReposUpdate)
	authed.HandleFunc("GET /reviews", s.handleReviewsList)
	authed.HandleFunc("GET /reviews/{id}", s.handleReviewDetail)
	authed.HandleFunc("POST /reviews/{id}/comments/{cid}/feedback", s.handleFeedbackSubmit)
	// API v1
	authed.HandleFunc("GET /api/v1/repos", s.apiListRepos)
	authed.HandleFunc("POST /api/v1/repos", s.apiUpsertRepo)
	authed.HandleFunc("GET /api/v1/reviews", s.apiListReviews)
	authed.HandleFunc("GET /api/v1/reviews/{id}", s.apiGetReview)
	authed.HandleFunc("POST /api/v1/reviews/{id}/comments/{cid}/feedback", s.apiSubmitFeedback)

	// Wrap the authed mux with Basic Auth (configured by caller).
	mux.Handle("/", AuthFromEnv(authed))

	// Static assets.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	return mux
}

// AuthFromEnv reads admin creds from env and wraps next with
// BasicAuth. If env vars are missing, returns next unchanged (this
// is intentional for tests / dev — production callers should call
// LoadAdminFromEnv separately to fail-fast at startup).
func AuthFromEnv(next http.Handler) http.Handler {
	user, pass, err := LoadAdminFromEnv()
	if err != nil {
		return next // dev mode: skip auth
	}
	return BasicAuth(user, pass)(next)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleMetrics is a placeholder; the cmd/reviewpilot server wires
// the actual Prometheus collector into GET /metrics directly.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "metrics handler not wired (see cmd/reviewpilot/server.go)", http.StatusNotImplemented)
}

// renderTemplate executes the named template against data.
func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) {
	if s.templates == nil {
		http.Error(w, "templates not initialized (use NewServerWithStore)", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

// requireStore returns a 500 if the server has no SQLite store
// (i.e., constructed via NewServer without a backing DB).
func (s *Server) requireStore(w http.ResponseWriter) bool {
	if s.store == nil {
		http.Error(w, "webdashboard: no SQLite store; use NewServerWithStore", http.StatusInternalServerError)
		return false
	}
	return true
}
