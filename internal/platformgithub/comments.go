package platformgithub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/bbbbbmy/reviewpilot/internal/reviewengine"
)

const (
	opListInline   = "github.list_inline_comments"
	opListSummary  = "github.list_summary_comments"
	opPostInline   = "github.post_inline_comment"
	opPostSummary  = "github.post_summary_comment"
	maxBodyBytes   = 10 << 20 // 10 MiB cap
	defaultTimeout = 30 * time.Second
)

// do executes an authenticated request and applies rate-limit-aware
// error classification. It does NOT auto-retry on 5xx; the caller
// decides via PlatformError.Kind.
func (a *GitHubAdapter) do(ctx context.Context, method, path string, body any, out any) (*http.Response, error) {
	u, err := url.Parse(a.cfg.BaseURL)
	if err != nil {
		return nil, &PlatformError{Op: path, Kind: kindUnknown, Message: "parse base url: " + err.Error()}
	}
	u.Path += path

	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, &PlatformError{Op: path, Kind: kindUnknown, Message: "encode body: " + err.Error()}
		}
		rdr = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return nil, &PlatformError{Op: path, Kind: kindUnknown, Message: "build request: " + err.Error()}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "reviewpilot/0.3")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, &PlatformError{Op: path, Kind: kindUnknown, Message: err.Error()}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		return nil, classifyHTTPError(path, resp.StatusCode, resp.Header)
	}
	if out != nil {
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if err != nil {
			_ = resp.Body.Close()
			return nil, &PlatformError{Op: path, Kind: kindUnknown, Message: "read body: " + err.Error()}
		}
		if err := json.Unmarshal(raw, out); err != nil {
			_ = resp.Body.Close()
			return nil, &PlatformError{Op: path, Kind: kindUnknown, Message: "decode body: " + err.Error()}
		}
	}
	return resp, nil
}

// splitRepo splits "owner/name" into (owner, name). Returns an
// error if the input doesn't look right.
func splitRepo(full string) (owner, name string, err error) {
	for i := 0; i < len(full); i++ {
		if full[i] == '/' {
			return full[:i], full[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid repo id: %q", full)
}

// inlineCommentBody is the JSON we POST to the review-comments endpoint.
type inlineCommentBody struct {
	CommitID string `json:"commit_id"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Body     string `json:"body"`
}

type issueCommentBody struct {
	Body string `json:"body"`
}

// rawInline mirrors the fields we consume from a review-comment GET
// response. The `body` field is the markdown comment text.
type rawInline struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

type rawIssueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// PostInlineComment posts a single inline comment on PR pr in
// repoID at the specified commit. commitSHA may be empty; in that
// case the caller must have pre-resolved it (GitHub API requires
// commit_id for inline comments).
func (a *GitHubAdapter) PostInlineComment(ctx context.Context, repoID string, pr int, commitSHA string, c Comment) error {
	if c.Path == "" || c.Body == "" {
		return &PlatformError{Op: opPostInline, Kind: kindProvider4xx, Message: "path and body required"}
	}
	owner, name, err := splitRepo(repoID)
	if err != nil {
		return &PlatformError{Op: opPostInline, Kind: kindProvider4xx, Message: err.Error()}
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", owner, name, pr)
	body := inlineCommentBody{CommitID: commitSHA, Path: c.Path, Line: c.Line, Body: c.Body}
	_, err = a.do(ctx, http.MethodPost, path, body, nil)
	return err
}

// PostSummaryComment posts a top-level PR summary comment (issue
// comments API).
func (a *GitHubAdapter) PostSummaryComment(ctx context.Context, repoID string, pr int, body string) error {
	if body == "" {
		return &PlatformError{Op: opPostSummary, Kind: kindProvider4xx, Message: "body required"}
	}
	owner, name, err := splitRepo(repoID)
	if err != nil {
		return &PlatformError{Op: opPostSummary, Kind: kindProvider4xx, Message: err.Error()}
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, name, pr)
	_, err = a.do(ctx, http.MethodPost, path, issueCommentBody{Body: body}, nil)
	return err
}

// ListComments returns inline + summary comments for a PR. Used by
// the review engine for dedup. Errors don't break the run; the
// engine treats an empty list as "no existing comments".
//
// The returned ExistingComment records have no Fingerprint; the
// engine computes its own from the body.
func (a *GitHubAdapter) ListComments(ctx context.Context, repoID string, pr int) ([]reviewengine.ExistingComment, error) {
	owner, name, err := splitRepo(repoID)
	if err != nil {
		return nil, &PlatformError{Op: opListInline, Kind: kindProvider4xx, Message: err.Error()}
	}

	// Inline comments.
	var inlineList []rawInline
	inlinePath := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", owner, name, pr)
	if _, err := a.do(ctx, http.MethodGet, inlinePath, nil, &inlineList); err != nil {
		return nil, err
	}

	// Summary comments.
	var issueList []rawIssueComment
	issuePath := fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, name, pr)
	if _, err := a.do(ctx, http.MethodGet, issuePath, nil, &issueList); err != nil {
		// Partial result is fine; engine can dedup against the inline
		// subset. Surface as a soft warn via the error.
		return convertToExisting(inlineList), err
	}

	out := make([]reviewengine.ExistingComment, 0, len(inlineList)+len(issueList))
	out = append(out, convertToExisting(inlineList)...)
	for _, c := range issueList {
		out = append(out, reviewengine.ExistingComment{FilePath: "", LineNumber: 0, Body: c.Body})
	}
	return out, nil
}

func convertToExisting(in []rawInline) []reviewengine.ExistingComment {
	out := make([]reviewengine.ExistingComment, 0, len(in))
	for _, c := range in {
		out = append(out, reviewengine.ExistingComment{FilePath: c.Path, LineNumber: c.Line, Body: c.Body})
	}
	return out
}
