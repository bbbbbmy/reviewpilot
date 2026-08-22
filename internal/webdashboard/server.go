// Package webdashboard exposes the HTTP server for ReviewPilot's Web UI
// and JSON API.
//
// In v0.0.1 it serves a single endpoint: `GET /healthz`, returning a
// JSON body `{"status":"ok"}`. Subsequent releases expand this to
// configuration / review-record / feedback endpoints per the
// web-dashboard capability spec.
package webdashboard

import (
	"encoding/json"
	"net/http"
)

// Server holds the HTTP server state.
type Server struct{}

// NewServer returns a Server with default configuration.
func NewServer() *Server {
	return &Server{}
}

// Handler returns the http.Handler that serves the API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	return mux
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
