package platformgithub

import (
	"errors"
	"os"
	"testing"
)

func TestLoadConfigFromEnv_OK(t *testing.T) {
	os.Setenv("RP_GITHUB_WEBHOOK_SECRET", "shh")
	os.Setenv("RP_GITHUB_TOKEN", "ghp_test")
	os.Setenv("RP_GITHUB_APP_ID", "12345")
	defer os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	defer os.Unsetenv("RP_GITHUB_TOKEN")
	defer os.Unsetenv("RP_GITHUB_APP_ID")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.WebhookSecret != "shh" {
		t.Fatalf("WebhookSecret=%s", cfg.WebhookSecret)
	}
	if cfg.Token != "ghp_test" {
		t.Fatalf("Token=%s", cfg.Token)
	}
	if cfg.AppID != 12345 {
		t.Fatalf("AppID=%d", cfg.AppID)
	}
	if cfg.BaseURL != "https://api.github.com" {
		t.Fatalf("BaseURL=%s", cfg.BaseURL)
	}
}

func TestLoadConfigFromEnv_CustomBaseURL(t *testing.T) {
	os.Setenv("RP_GITHUB_BASE_URL", "https://github.example.com/api/v3")
	os.Setenv("RP_GITHUB_WEBHOOK_SECRET", "shh")
	os.Setenv("RP_GITHUB_TOKEN", "tok")
	defer os.Unsetenv("RP_GITHUB_BASE_URL")
	defer os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	defer os.Unsetenv("RP_GITHUB_TOKEN")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.BaseURL != "https://github.example.com/api/v3" {
		t.Fatalf("BaseURL=%s", cfg.BaseURL)
	}
}

func TestLoadConfigFromEnv_MissingWebhook(t *testing.T) {
	os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	os.Setenv("RP_GITHUB_TOKEN", "tok")
	defer os.Unsetenv("RP_GITHUB_TOKEN")
	_, err := LoadConfigFromEnv()
	if err == nil {
		t.Fatal("expected missing webhook secret error")
	}
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadConfigFromEnv_MissingToken(t *testing.T) {
	os.Setenv("RP_GITHUB_WEBHOOK_SECRET", "shh")
	os.Unsetenv("RP_GITHUB_TOKEN")
	defer os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	_, err := LoadConfigFromEnv()
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadConfigFromEnv_BothMissing(t *testing.T) {
	os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	os.Unsetenv("RP_GITHUB_TOKEN")
	_, err := LoadConfigFromEnv()
	if !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadConfigFromEnv_AppIDParseError(t *testing.T) {
	// Non-numeric AppID should be silently ignored (currently informational).
	os.Setenv("RP_GITHUB_APP_ID", "abc")
	os.Setenv("RP_GITHUB_WEBHOOK_SECRET", "shh")
	os.Setenv("RP_GITHUB_TOKEN", "tok")
	defer os.Unsetenv("RP_GITHUB_APP_ID")
	defer os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	defer os.Unsetenv("RP_GITHUB_TOKEN")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.AppID != 0 {
		t.Fatalf("AppID=%d, want 0 (parse failed)", cfg.AppID)
	}
}

func TestLoadConfigFromEnv_PrivateKey(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----"
	os.Setenv("RP_GITHUB_APP_PRIVATE_KEY", pem)
	os.Setenv("RP_GITHUB_WEBHOOK_SECRET", "shh")
	os.Setenv("RP_GITHUB_TOKEN", "tok")
	defer os.Unsetenv("RP_GITHUB_APP_PRIVATE_KEY")
	defer os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	defer os.Unsetenv("RP_GITHUB_TOKEN")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if string(cfg.PrivateKeyPEM) != pem {
		t.Fatalf("PrivateKeyPEM=%q", cfg.PrivateKeyPEM)
	}
}

func TestNewAdapterFromEnv_Missing(t *testing.T) {
	os.Unsetenv("RP_GITHUB_WEBHOOK_SECRET")
	os.Unsetenv("RP_GITHUB_TOKEN")
	if _, err := NewAdapterFromEnv(); !errors.Is(err, ErrMissingCredential) {
		t.Fatalf("err=%v", err)
	}
}
