package platformgithub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func makeSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature_OK(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	sig := makeSignature("shh", body)
	if err := VerifySignature("shh", body, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifySignature_Missing(t *testing.T) {
	if err := VerifySignature("shh", []byte("x"), ""); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifySignature_Mismatch(t *testing.T) {
	body := []byte("hello")
	if err := VerifySignature("shh", body, makeSignature("wrong", body)); !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestVerifySignature_BadPrefix(t *testing.T) {
	body := []byte("x")
	err := VerifySignature("shh", body, "sha1=abc")
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("err=%v", err)
	}
}

func TestParsePullRequestEvent_OK(t *testing.T) {
	body := []byte(`{
		"action": "opened",
		"number": 42,
		"pull_request": {"head": {"sha": "deadbeef"}},
		"repository": {"full_name": "bbbbbmy/reviewpilot"},
		"installation": {"id": 12345},
		"sender": {"login": "alice"}
	}`)
	ev, err := ParsePullRequestEvent(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ev.Action != "opened" || ev.PRNumber != 42 {
		t.Fatalf("ev=%+v", ev)
	}
	if ev.RepoFullName != "bbbbbmy/reviewpilot" {
		t.Fatalf("RepoFullName=%s", ev.RepoFullName)
	}
	if ev.CommitSHA != "deadbeef" {
		t.Fatalf("CommitSHA=%s", ev.CommitSHA)
	}
	if ev.InstallationID != 12345 {
		t.Fatalf("InstallationID=%d", ev.InstallationID)
	}
	if ev.Sender != "alice" {
		t.Fatalf("Sender=%s", ev.Sender)
	}
}

func TestParsePullRequestEvent_UnsupportedAction(t *testing.T) {
	body := []byte(`{"action": "closed", "number": 1, "pull_request":{"head":{"sha":"x"}}, "repository":{"full_name":"a/b"}}`)
	if _, err := ParsePullRequestEvent(body); err == nil {
		t.Fatal("expected error for unsupported action")
	}
}

func TestParsePullRequestEvent_NonPRObject(t *testing.T) {
	body := []byte(`{"action":"opened","number":1,"issue":{"id":1},"repository":{"full_name":"a/b"}}`)
	if _, err := ParsePullRequestEvent(body); err == nil {
		t.Fatal("expected error for non-PR event")
	}
}

func TestParsePullRequestEvent_Malformed(t *testing.T) {
	if _, err := ParsePullRequestEvent([]byte("not json")); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParsePullRequestEvent_AllActionableActions(t *testing.T) {
	for _, action := range []string{"opened", "synchronize", "reopened", "ready_for_review"} {
		body := []byte(`{"action":"` + action + `","number":1,"pull_request":{"head":{"sha":"x"}},"repository":{"full_name":"a/b"}}`)
		if _, err := ParsePullRequestEvent(body); err != nil {
			t.Fatalf("action %s: %v", action, err)
		}
	}
}

func TestVerifyAndParse_OK(t *testing.T) {
	body := []byte(`{"action":"opened","number":1,"pull_request":{"head":{"sha":"x"}},"repository":{"full_name":"a/b"}}`)
	sig := makeSignature("s", body)
	ev, err := VerifyAndParse("s", body, sig)
	if err != nil {
		t.Fatalf("VerifyAndParse: %v", err)
	}
	if ev.PRNumber != 1 {
		t.Fatalf("PRNumber=%d", ev.PRNumber)
	}
}

func TestVerifyAndParse_BadSig(t *testing.T) {
	body := []byte(`{"action":"opened","number":1}`)
	_, err := VerifyAndParse("s", body, "sha256=bad")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("err=%v", err)
	}
}
