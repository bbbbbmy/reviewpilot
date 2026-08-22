// Package platformgithub implements the GitHub platform adapter for
// ReviewPilot.
//
// It satisfies reviewengine.PlatformAdapter (ListComments) and adds
// the GitHub-App-specific surface (webhook signature verification,
// inline / summary comment posting, rate-limit-aware HTTP).
//
// Phase 4 supports two auth modes via Config.Token:
//
//  1. PAT (Personal Access Token) — for dev / smoke tests
//  2. Installation token — production path via GitHub App; full JWT
//     flow is implemented in a follow-up phase. For now, the server
//     accepts a pre-minted installation token in `RP_GITHUB_TOKEN`.
package platformgithub

import (
	"net/http"
)

// Config wires up a GitHubAdapter. Zero values fall back to defaults.
type Config struct {
	// AppID is the GitHub App ID. Required for production (signature
	// verification uses the same shared secret, but AppID is reserved
	// for future App-installation-token exchange).
	AppID int64
	// PrivateKeyPEM is the GitHub App's RSA private key in PEM form.
	// Currently unused (PAT path); reserved for JWT signing.
	PrivateKeyPEM []byte
	// WebhookSecret is the shared secret used to verify the
	// `X-Hub-Signature-256` header on inbound webhooks.
	WebhookSecret string
	// Token is the bearer token used on outbound API calls. In
	// production this is a GitHub App installation token; in dev it
	// can be a Personal Access Token.
	Token string
	// HTTPClient is optional. Default: &http.Client{}.
	HTTPClient *http.Client
	// BaseURL is the GitHub API root. Default: "https://api.github.com".
	BaseURL string
}

// PullRequestEvent is the minimal subset of GitHub's
// `pull_request` webhook payload that the engine consumes.
type PullRequestEvent struct {
	Action         string // opened | synchronize | reopened
	PRNumber       int
	RepoFullName   string // "owner/repo"
	CommitSHA      string // pull_request.head.sha
	InstallationID int64  // payload.installation.id (0 if absent)
	Sender         string // payload.sender.login (for logging)
}

// Comment describes one line-level or summary comment to post.
// Line == 0 → summary comment (issue comments API).
type Comment struct {
	Path string
	Line int
	Body string
}

// parseErrorBody mirrors GitHub's error response shape.

// platformErrorKind is the public classification for a platform-side
// failure (similar to llmopenai.Kind).
type platformErrorKind string

const (
	kindRateLimited platformErrorKind = "rate_limited"
	kindProvider5xx platformErrorKind = "provider_5xx"
	kindProvider4xx platformErrorKind = "provider_4xx"
	kindUnknown     platformErrorKind = "unknown"
)

// PlatformError is the normalized error type for this package.
type PlatformError struct {
	Op         string
	Kind       platformErrorKind
	StatusCode int
	Message    string
	RetryAfter string // raw Retry-After header, if any
}

func (e *PlatformError) Error() string {
	if e == nil {
		return "<nil platform error>"
	}
	return e.Op + ": " + e.Message + " (kind=" + string(e.Kind) + ", status=" + itoa(e.StatusCode) + ")"
}

func itoa(n int) string {
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
