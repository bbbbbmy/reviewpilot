package reviewengine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSplit_WholePRFitsBudget(t *testing.T) {
	req := Request{Diff: "diff --git a/x.go b/x.go\n@@\n+small", SystemPrompt: "sys"}
	chunks, err := Split(req, 100_000)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d", len(chunks))
	}
	if chunks[0].FilePath != "<pr>" {
		t.Fatalf("FilePath=%s", chunks[0].FilePath)
	}
}

func TestSplit_FileLevelByMarkers(t *testing.T) {
	// Use a diff large enough to exceed the 60% effective budget,
	// so Split falls into per-file mode rather than whole-PR.
	lines := []string{
		"diff --git a/a.go b/a.go",
		"@@",
		"+aaa",
	}
	for i := 0; i < 200; i++ {
		lines = append(lines, "+x")
	}
	for i := 0; i < 3; i++ {
		lines = append(lines, fmt.Sprintf("diff --git a/letter%d.go b/letter%d.go", i, i))
		lines = append(lines, "@@")
		lines = append(lines, "+x")
	}
	diff := strings.Join(lines, "\n")

	chunks, err := Split(Request{Diff: diff}, 50)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	// big a.go file: ~800 chars ⇒ ~200 tokens > budget 30 (50*0.6).
	// Each small letter*.go file is ~30 chars ⇒ ≤ budget ⇒ kept.
	if len(chunks) != 3 {
		t.Fatalf("chunks=%d, want 3", len(chunks))
	}
}

func TestSplit_DropsOversizedFile(t *testing.T) {
	big := strings.Repeat("x", 100_000)
	diff := "diff --git a/big.go b/big.go\n@@\n+" + big + "\n" +
		"diff --git a/small.go b/small.go\n@@\n+small\n"

	chunks, err := Split(Request{Diff: diff}, 5_000)
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	// big.go dropped; only small.go kept.
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d, want 1 (big.go dropped)", len(chunks))
	}
	if chunks[0].FilePath != "small.go" {
		t.Fatalf("kept=%s, want small.go", chunks[0].FilePath)
	}
}

func TestSplit_NoChunkFitsBudget_ErrNoChunkFitsBudget(t *testing.T) {
	big := strings.Repeat("x", 200_000)
	diff := "diff --git a/big.go b/big.go\n@@\n+" + big + "\n"
	_, err := Split(Request{Diff: diff}, 5_000)
	if !errors.Is(err, ErrNoChunkFitsBudget) {
		t.Fatalf("err=%v, want ErrNoChunkFitsBudget", err)
	}
}

func TestSplit_ZeroBudget_ErrInvalidBudget(t *testing.T) {
	_, err := Split(Request{Diff: "x"}, 0)
	if !errors.Is(err, ErrInvalidBudget) {
		t.Fatalf("err=%v, want ErrInvalidBudget", err)
	}
}
