package platformgithub

import (
	"errors"
	"net/http"
	"os"
)

// ErrMissingCredential is returned by LoadConfigFromEnv when a
// required environment variable is not set.
var ErrMissingCredential = errors.New("platformgithub: required environment variable is missing")

// LoadConfigFromEnv reads GitHub credentials from environment
// variables and returns a Config. Required vars:
//
//   - RP_GITHUB_APP_ID              (numeric; required for prod, optional for PAT-only dev)
//   - RP_GITHUB_APP_PRIVATE_KEY     (PEM; optional in v0.3.0; reserved for JWT signing)
//   - RP_GITHUB_WEBHOOK_SECRET      (required for webhook signature verification)
//   - RP_GITHUB_TOKEN               (bearer token; required for outbound API calls)
//
// Returns ErrMissingCredential listing the first missing var.
func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL: os.Getenv("RP_GITHUB_BASE_URL"),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	cfg.WebhookSecret = os.Getenv("RP_GITHUB_WEBHOOK_SECRET")
	cfg.Token = os.Getenv("RP_GITHUB_TOKEN")
	if v := os.Getenv("RP_GITHUB_APP_ID"); v != "" {
		// ignore parse error — AppID is currently informational
		var n int64
		for _, c := range v {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int64(c-'0')
		}
		cfg.AppID = n
	}
	if v := os.Getenv("RP_GITHUB_APP_PRIVATE_KEY"); v != "" {
		cfg.PrivateKeyPEM = []byte(v)
	}

	// Required: WebhookSecret + Token. AppID/PEM are reserved.
	if cfg.WebhookSecret == "" {
		return Config{}, missing("RP_GITHUB_WEBHOOK_SECRET")
	}
	if cfg.Token == "" {
		return Config{}, missing("RP_GITHUB_TOKEN")
	}

	cfg.HTTPClient = &http.Client{}
	return cfg, nil
}

// missing wraps ErrMissingCredential with a useful message.
func missing(varName string) error {
	return &missingError{VarName: varName}
}

type missingError struct {
	VarName string
}

func (e *missingError) Error() string {
	return ErrMissingCredential.Error() + ": " + e.VarName
}

func (e *missingError) Unwrap() error { return ErrMissingCredential }
