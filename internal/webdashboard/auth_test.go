package webdashboard

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLoadAdminFromEnv_OK(t *testing.T) {
	os.Setenv("RP_ADMIN_USER", "admin")
	os.Setenv("RP_ADMIN_PASS", "shh")
	defer os.Unsetenv("RP_ADMIN_USER")
	defer os.Unsetenv("RP_ADMIN_PASS")
	u, p, err := LoadAdminFromEnv()
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if u != "admin" || p != "shh" {
		t.Fatalf("u=%s p=%s", u, p)
	}
}

func TestLoadAdminFromEnv_MissingUser(t *testing.T) {
	os.Unsetenv("RP_ADMIN_USER")
	os.Setenv("RP_ADMIN_PASS", "p")
	defer os.Unsetenv("RP_ADMIN_PASS")
	if _, _, err := LoadAdminFromEnv(); err != ErrMissingAdmin {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadAdminFromEnv_MissingPass(t *testing.T) {
	os.Setenv("RP_ADMIN_USER", "u")
	os.Unsetenv("RP_ADMIN_PASS")
	defer os.Unsetenv("RP_ADMIN_USER")
	if _, _, err := LoadAdminFromEnv(); err != ErrMissingAdmin {
		t.Fatalf("err=%v", err)
	}
}

func TestBasicAuth_OK(t *testing.T) {
	h := BasicAuth("u", "p")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("u", "p")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestBasicAuth_WrongCreds(t *testing.T) {
	h := BasicAuth("u", "p")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("u", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("WWW-Authenticate header missing")
	}
}

func TestBasicAuth_NoHeader(t *testing.T) {
	h := BasicAuth("u", "p")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
