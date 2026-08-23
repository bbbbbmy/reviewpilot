package webdashboard

import (
	"crypto/subtle"
	"errors"
	"net/http"
)

// ErrMissingAdmin is returned by LoadAdminFromEnv when the required
// admin user / password env vars are missing.
var ErrMissingAdmin = errors.New("webdashboard: RP_ADMIN_USER and RP_ADMIN_PASS are required")

// LoadAdminFromEnv reads Basic Auth credentials from the
// RP_ADMIN_USER and RP_ADMIN_PASS env vars. Returns ErrMissingAdmin
// if either is empty.
func LoadAdminFromEnv() (user, pass string, err error) {
	user = getenv("RP_ADMIN_USER")
	pass = getenv("RP_ADMIN_PASS")
	if user == "" || pass == "" {
		return "", "", ErrMissingAdmin
	}
	return user, pass, nil
}

// BasicAuth returns a middleware that requires `Authorization: Basic`
// on every request. Path exceptions (e.g., /healthz, /metrics) MUST be
// registered before the auth-wrapped mux is applied.
func BasicAuth(user, pass string) func(http.Handler) http.Handler {
	if user == "" || pass == "" {
		panic("BasicAuth: empty credentials (server should refuse to start)")
	}
	userBytes := []byte(user)
	passBytes := []byte(pass)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(u), userBytes) != 1 ||
				subtle.ConstantTimeCompare([]byte(p), passBytes) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="reviewpilot"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getenv is a tiny indirection so tests can override env without
// touching os.Getenv (kept as a separate file-level var to avoid
// import cycles with the test package).
func getenv(name string) string { return osGetenv(name) }
