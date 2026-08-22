package reviewengine

import "testing"

func TestFingerprint_StableAcrossCaseAndWhitespace(t *testing.T) {
	a := Fingerprint("This is a comment")
	b := Fingerprint("this   is a COMMENT")
	if a != b || a == "" {
		t.Fatalf("expected same fp, got %q vs %q", a, b)
	}
}

func TestFingerprint_Empty(t *testing.T) {
	if got := Fingerprint(""); got != "" {
		t.Fatalf("empty body should produce empty fp, got %q", got)
	}
}

func TestDedup_ExactFingerprintDrops(t *testing.T) {
	fp := Fingerprint("hello world")
	out := Dedup([]Comment{{Body: "hello world", Fingerprint: fp}},
		[]ExistingComment{{Body: "hello world", Fingerprint: fp}})
	if len(out) != 0 {
		t.Fatalf("expected drop, got %d", len(out))
	}
}

func TestDedup_KeepsDifferentComments(t *testing.T) {
	out := Dedup([]Comment{{Body: "first", Fingerprint: "a"}},
		[]ExistingComment{{Body: "totally different", Fingerprint: "b"}})
	if len(out) != 1 {
		t.Fatalf("expected keep, got %d", len(out))
	}
}

func TestDedup_DropsByTokenSimilarity(t *testing.T) {
	// existing = 12 tokens; incoming = same 12 + 1 new token.
	// jaccard = 12/13 ≈ 0.923 > 0.85.
	existing := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima"
	incoming := "alpha bravo charlie delta echo foxtrot golf hotel india juliet kilo lima mike"
	fp := Fingerprint(existing)
	out := Dedup([]Comment{{Body: incoming, Fingerprint: Fingerprint(incoming)}},
		[]ExistingComment{{Body: existing, Fingerprint: fp}})
	if len(out) != 0 {
		t.Fatalf("expected drop by similarity, got %d comments", len(out))
	}
}

func TestDedup_LocalDedupWithinBatch(t *testing.T) {
	out := Dedup(
		[]Comment{
			{Body: "hello world", Fingerprint: "abc"},
			{Body: "hello world", Fingerprint: "abc"},
		},
		nil,
	)
	if len(out) != 1 {
		t.Fatalf("expected local dedup, got %d", len(out))
	}
}

func TestDedup_EmptyInput(t *testing.T) {
	if out := Dedup(nil, nil); out != nil {
		t.Fatalf("got %v", out)
	}
}
