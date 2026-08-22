package platformgithub

import (
	"net/http"
)

// GitHubAdapter is the production implementation of
// reviewengine.PlatformAdapter for GitHub.
type GitHubAdapter struct {
	cfg    Config
	client *http.Client
}

// NewAdapter returns a GitHubAdapter. The cfg.Token must be set;
// cfg.WebhookSecret may be empty if signature verification is done
// at the HTTP layer (it is, in cmd/reviewpilot/server.go).
func NewAdapter(cfg Config) (*GitHubAdapter, error) {
	if cfg.Token == "" {
		return nil, &PlatformError{Op: "new", Kind: kindProvider4xx, Message: "Token required"}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.github.com"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultTimeout}
	}
	return &GitHubAdapter{cfg: cfg, client: cfg.HTTPClient}, nil
}

// NewAdapterFromEnv is shorthand for LoadConfigFromEnv + NewAdapter.
// Server startup calls this; any error fails the server.
func NewAdapterFromEnv() (*GitHubAdapter, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewAdapter(cfg)
}

// VerifySignature exposes the package-level VerifySignature so the
// HTTP layer can validate without importing the unexported helper.
func (a *GitHubAdapter) VerifySignature(body []byte, signatureHeader string) error {
	return VerifySignature(a.cfg.WebhookSecret, body, signatureHeader)
}

// ParseEvent exposes the package-level parser.
func (a *GitHubAdapter) ParseEvent(body []byte) (*PullRequestEvent, error) {
	return ParsePullRequestEvent(body)
}
