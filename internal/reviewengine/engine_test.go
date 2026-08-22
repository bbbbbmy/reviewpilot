package reviewengine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
)

// fakeLLM is a programmable llmopenai.LLMProvider for tests. Each
// `respond` in the queue is returned on successive Chat calls; if
// the queue is empty, fakeLLM returns the last response (or an error).
type fakeLLM struct {
	mu    sync.Mutex
	queue []fakeResp
	last  fakeResp
	calls int
}

type fakeResp struct {
	body  string
	usage llmopenai.Usage
	err   error
}

func (f *fakeLLM) enqueue(r fakeResp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, r)
}

func (f *fakeLLM) Chat(_ context.Context, _ llmopenai.ChatRequest) (llmopenai.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.queue) == 0 {
		if f.last.err != nil {
			return llmopenai.ChatResponse{}, f.last.err
		}
		return llmopenai.ChatResponse{Content: f.last.body, Usage: f.last.usage, FinishReason: llmopenai.FinishStop}, nil
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	f.last = r
	if r.err != nil {
		return llmopenai.ChatResponse{}, r.err
	}
	return llmopenai.ChatResponse{Content: r.body, Usage: r.usage, FinishReason: llmopenai.FinishStop}, nil
}

// fakePlatform records calls and returns a preconfigured existing-comment list.
type fakePlatform struct {
	mu        sync.Mutex
	existing  []ExistingComment
	listErr   error
	listCalls int
}

func (p *fakePlatform) ListComments(_ context.Context, _ string, _ int) ([]ExistingComment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.listCalls++
	return p.existing, p.listErr
}

func newTestEngine(t *testing.T, llm *fakeLLM, plat *fakePlatform, fb FeedbackStore) (Engine, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	eng, err := NewDefaultEngine(EngineDeps{
		LLM:                 llm,
		Platform:            plat,
		Feedback:            fb,
		Metrics:             NewMetrics(reg),
		MaxInputTokensPerPR: 10_000,
		FeedbackWindowDays:  30,
	})
	if err != nil {
		t.Fatalf("NewDefaultEngine: %v", err)
	}
	return eng, reg
}

func TestEngine_Run_OK_EndToEnd(t *testing.T) {
	diff := "diff --git a/internal/foo/foo.go b/internal/foo/foo.go\n--- a/internal/foo/foo.go\n+++ b/internal/foo/foo.go\n@@ -1,3 +1,5 @@\n package foo\n+// TODO\n func Bar() {}\n+var _ = 1\n"
	llm := &fakeLLM{}
	llm.enqueue(fakeResp{body: "internal/foo/foo.go: this looks fine.", usage: llmopenai.Usage{TokensIn: 200, TokensOut: 30}})
	plat := &fakePlatform{}
	eng, _ := newTestEngine(t, llm, plat, NewInMemoryFeedbackStore())

	res, err := eng.Run(context.Background(), Request{RepoID: "bbbbbmy/reviewpilot", PRNumber: 1, Diff: diff})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusDone {
		t.Fatalf("Status=%s, want done", res.Status)
	}
	if len(res.Comments) != 1 {
		t.Fatalf("Comments=%d, want 1", len(res.Comments))
	}
	if res.TokenUsage.TokensIn != 200 || res.TokenUsage.TokensOut != 30 {
		t.Fatalf("TokenUsage=%+v", res.TokenUsage)
	}
}

func TestEngine_Run_DedupDropsExisting(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n@@\n+new line\n"
	llm := &fakeLLM{}
	llm.enqueue(fakeResp{body: "this is a comment"})
	plat := &fakePlatform{
		existing: []ExistingComment{{FilePath: "x.go", Body: "this is a comment", Fingerprint: Fingerprint("this is a comment")}},
	}
	eng, _ := newTestEngine(t, llm, plat, NewInMemoryFeedbackStore())

	res, err := eng.Run(context.Background(), Request{RepoID: "r", PRNumber: 1, Diff: diff})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Comments) != 0 {
		t.Fatalf("expected dedup to drop, got %d", len(res.Comments))
	}
}

func TestEngine_Run_FeedbackInjectedOnSecondRun(t *testing.T) {
	diff := "diff --git a/y.go b/y.go\n@@\n+x\n"
	llm := &fakeLLM{}
	llm.enqueue(fakeResp{body: "y.go: ok"})
	llm.enqueue(fakeResp{body: "y.go: ok"})
	store := NewInMemoryFeedbackStore()
	_ = store.Add(context.Background(), "r", Feedback{Rating: RatingUp, Text: "be terse", CreatedAt: time.Now()})
	_ = store.Add(context.Background(), "r", Feedback{Rating: RatingDown, Text: "no emoji", CreatedAt: time.Now()})

	eng, _ := newTestEngine(t, llm, &fakePlatform{}, store)

	for i := 0; i < 2; i++ {
		if _, err := eng.Run(context.Background(), Request{RepoID: "r", PRNumber: 1, Diff: diff}); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}
	if llm.calls < 2 {
		t.Fatalf("expected ≥2 calls, got %d", llm.calls)
	}
}

func TestEngine_Run_SkippedWhenSingleFileTooBig(t *testing.T) {
	huge := strings.Repeat("a", 50_000)
	diff := "diff --git a/huge.go b/huge.go\n@@\n+" + huge + "\n"
	llm := &fakeLLM{}
	eng, _ := newTestEngine(t, llm, &fakePlatform{}, NewInMemoryFeedbackStore())

	res, err := eng.Run(context.Background(), Request{RepoID: "r", PRNumber: 1, Diff: diff})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != StatusSkipped {
		t.Fatalf("Status=%s, want skipped", res.Status)
	}
	if llm.calls != 0 {
		t.Fatalf("expected 0 LLM calls on skip, got %d", llm.calls)
	}
}

func TestEngine_Run_FailureIsolation_LLMError5xx(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\n@@\n+y\n"
	llm := &fakeLLM{}
	llm.enqueue(fakeResp{err: &llmopenai.LLMError{Op: "openai.chat", Kind: llmopenai.KindProvider5xx, StatusCode: 500, Message: "boom"}})
	eng, _ := newTestEngine(t, llm, &fakePlatform{}, NewInMemoryFeedbackStore())

	res, err := eng.Run(context.Background(), Request{RepoID: "r", PRNumber: 1, Diff: diff})
	if err != nil {
		t.Fatalf("Run returned err (failure isolation broken): %v", err)
	}
	if res.Status != StatusDone {
		t.Fatalf("Status=%s, want done (isolation)", res.Status)
	}
}

func TestNewDefaultEngine_RequiresLLM(t *testing.T) {
	if _, err := NewDefaultEngine(EngineDeps{Platform: &fakePlatform{}}); !errors.Is(err, ErrMissingLLM) {
		t.Fatalf("err=%v, want ErrMissingLLM", err)
	}
}

func TestNewDefaultEngine_PlatformOptionalForCLI(t *testing.T) {
	eng, err := NewDefaultEngine(EngineDeps{LLM: &fakeLLM{}})
	if err != nil {
		t.Fatalf("nil platform should be allowed (CLI mode): %v", err)
	}
	if eng == nil {
		t.Fatal("expected non-nil engine")
	}
}
