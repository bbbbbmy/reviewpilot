package platformgithub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// ErrMissingSignature is returned when the request has no signature
// header.
var ErrMissingSignature = errors.New("platformgithub: missing signature header")

// ErrSignatureMismatch is returned when the signature header does
// not match the expected HMAC.
var ErrSignatureMismatch = errors.New("platformgithub: signature mismatch")

// VerifySignature verifies the `X-Hub-Signature-256` header value
// (e.g., "sha256=abcdef0123…") against the HMAC-SHA256 of body
// using secret. Returns nil on success.
//
// Uses hmac.Equal for constant-time comparison.
func VerifySignature(secret string, body []byte, header string) error {
	if header == "" {
		return ErrMissingSignature
	}
	// Header is "sha256=<hex>".
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return ErrSignatureMismatch
	}
	sig, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return ErrSignatureMismatch
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return ErrSignatureMismatch
	}
	return nil
}

// rawPullRequestEvent is the GitHub webhook payload shape we care
// about. We unmarshal only the fields the engine uses.
type rawPullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

// ParsePullRequestEvent parses a GitHub pull_request webhook body
// into a PullRequestEvent. Other event types (issues, push, …)
// return an error so the caller can short-circuit.
func ParsePullRequestEvent(body []byte) (*PullRequestEvent, error) {
	// Distinguish pull_request from issue events by checking for the
	// `pull_request` top-level field. Issue events carry `issue`
	// instead.
	var probe struct {
		PullRequest json.RawMessage `json:"pull_request"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, err
	}
	if len(probe.PullRequest) == 0 {
		return nil, errors.New("platformgithub: not a pull_request event (no pull_request field)")
	}

	var raw rawPullRequestEvent
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.Action == "" || raw.Number == 0 {
		return nil, errors.New("platformgithub: not a pull_request event (no action or number)")
	}
	// GitHub's pull_request events are only actionable for these actions.
	switch raw.Action {
	case "opened", "synchronize", "reopened", "ready_for_review":
		// pass
	default:
		return nil, errors.New("platformgithub: unsupported action: " + raw.Action)
	}
	return &PullRequestEvent{
		Action:         raw.Action,
		PRNumber:       raw.Number,
		RepoFullName:   raw.Repository.FullName,
		CommitSHA:      raw.PullRequest.Head.SHA,
		InstallationID: raw.Installation.ID,
		Sender:         raw.Sender.Login,
	}, nil
}

// VerifyAndParse combines VerifySignature + ParsePullRequestEvent.
// Server handlers should call this and on error return HTTP 401.
func VerifyAndParse(secret string, body []byte, signatureHeader string) (*PullRequestEvent, error) {
	if err := VerifySignature(secret, body, signatureHeader); err != nil {
		return nil, err
	}
	return ParsePullRequestEvent(body)
}
