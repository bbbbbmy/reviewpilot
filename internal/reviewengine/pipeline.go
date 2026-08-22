package reviewengine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
)

// PipelineDeps mirrors EngineDeps but is local to the pipeline
// orchestrator. We keep them separate so callers may construct a
// Pipeline directly (e.g., custom metric backends) without going
// through DefaultEngine.
type PipelineDeps struct {
	LLM      llmopenai.LLMProvider
	Platform PlatformAdapter
	Feedback FeedbackStore
	Metrics  *Metrics
	Logger   *slog.Logger
	Budget   int // tokens
	Window   int // days
}

// Pipeline implements the 5-step orchestrator.
//
// Order (per spec):
//
//  1. split — turn diff into chunks
//  2. context — assemble ChatRequest with system + diff + recent feedback
//  3. llm — call provider
//  4. dedup — drop comments matching existing platform comments
//  5. feedback — already injected upstream (step 2 reads from store);
//     this step updates metrics / persists nothing in MVP
type Pipeline struct {
	deps PipelineDeps
}

// NewPipeline returns a Pipeline ready to Run.
func NewPipeline(deps PipelineDeps) *Pipeline {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Budget <= 0 {
		deps.Budget = 100_000
	}
	if deps.Window <= 0 {
		deps.Window = 30
	}
	return &Pipeline{deps: deps}
}

// Run executes the 5-step pipeline.
func (p *Pipeline) Run(ctx context.Context, req Request) (Result, error) {
	result := Result{
		Status:        StatusRunning,
		StepDurations: map[StepName]time.Duration{},
	}

	start := time.Now()

	// Step 1: split
	chunks, splitErr := timed(StepSplit, p.deps.Metrics, func() ([]Chunk, error) {
		return Split(req, p.deps.Budget)
	})
	result.StepDurations[StepSplit] = time.Since(start)
	if splitErr != nil {
		return p.handleSplitFailure(result, splitErr)
	}

	// Step 2 + 3: context + llm, per chunk
	allComments := make([]Comment, 0)
	totalUsage := TokenUsage{}
	for _, ch := range chunks {
		// Step 2: assemble ChatRequest with feedback context.
		t0 := time.Now()
		chatReq, ctxErr := p.assemble(ctx, req, ch)
		result.StepDurations[StepContext] += time.Since(t0)
		if ctxErr != nil {
			p.deps.Logger.Warn("assemble chat request failed",
				"file", ch.FilePath, "err", ctxErr)
			continue
		}
		p.deps.Metrics.ObserveStep(StepContext, time.Since(t0).Seconds())

		// Step 3: llm
		t1 := time.Now()
		resp, llmErr := p.deps.LLM.Chat(ctx, chatReq)
		result.StepDurations[StepLLM] += time.Since(t1)
		if llmErr != nil {
			p.deps.Logger.Warn("llm chat failed",
				"file", ch.FilePath, "err", llmErr)
			continue
		}
		p.deps.Metrics.ObserveStep(StepLLM, time.Since(t1).Seconds())
		p.deps.Metrics.ObserveTokens(resp.Usage.TokensIn, resp.Usage.TokensOut)
		totalUsage.Add(TokenUsage{
			TokensIn:  resp.Usage.TokensIn,
			TokensOut: resp.Usage.TokensOut,
		})

		// Parse response into Comments. MVP: split response body by
		// double-newline (each block is one comment). Real implementation
		// will use a structured output format.
		allComments = append(allComments, parseLLMComments(ch.FilePath, resp.Content)...)
	}

	// Step 4: dedup against existing platform comments. Platform
	// is optional (CLI mode passes nil) — fall back to local-only
	// dedup in that case.
	t2 := time.Now()
	var existing []ExistingComment
	if p.deps.Platform != nil {
		var listErr error
		existing, listErr = p.deps.Platform.ListComments(ctx, req.RepoID, req.PRNumber)
		if listErr != nil {
			p.deps.Logger.Warn("list existing comments failed", "err", listErr)
			existing = nil
		}
	}
	result.StepDurations[StepDedup] = time.Since(t2)
	p.deps.Metrics.ObserveStep(StepDedup, result.StepDurations[StepDedup].Seconds())

	filtered := Dedup(allComments, existing)
	result.Comments = filtered
	result.TokenUsage = totalUsage

	// Step 5: feedback — already injected at step 2; observe timing
	result.StepDurations[StepFeedback] = 0
	p.deps.Metrics.ObserveStep(StepFeedback, 0)

	result.Status = StatusDone
	p.deps.Metrics.ObserveStatus(StatusDone)
	return result, nil
}

// handleSplitFailure translates a Split error into the appropriate
// Result.Status per the failure decision table.
func (p *Pipeline) handleSplitFailure(result Result, err error) (Result, error) {
	decision := Classify(err)
	result.Err = err
	result.Reason = ShortReason(err)

	switch decision {
	case DecisionRetry:
		// Retry would re-split with a smaller chunk — out of scope for
		// MVP. Treat as Failed for now.
		result.Status = StatusFailed
	case DecisionSkip:
		result.Status = StatusSkipped
	case DecisionEscalate:
		result.Status = StatusFailed
	}
	p.deps.Metrics.ObserveStatus(result.Status)
	return result, nil
}

// assemble constructs a ChatRequest for one chunk. Pulls recent
// feedback from the store and prepends a feedback summary to the
// system prompt.
func (p *Pipeline) assemble(ctx context.Context, req Request, ch Chunk) (llmopenai.ChatRequest, error) {
	window := time.Duration(p.deps.Window) * 24 * time.Hour
	fbs, err := p.deps.Feedback.RecentForRepo(ctx, req.RepoID, window)
	if err != nil {
		return llmopenai.ChatRequest{}, fmt.Errorf("feedback recent: %w", err)
	}

	system := req.SystemPrompt
	if system == "" {
		system = defaultSystemPrompt
	}
	if summary := summarizeFeedback(fbs); summary != "" {
		system = system + "\n\nRecent reviewer feedback (last " + fmt.Sprintf("%d", p.deps.Window) + " days):\n" + summary
	}

	user := fmt.Sprintf("File: %s\n```diff\n%s\n```", ch.FilePath, ch.Body)

	return llmopenai.ChatRequest{
		Model: "", // provider default
		Messages: []llmopenai.Message{
			{Role: llmopenai.RoleSystem, Content: system},
			{Role: llmopenai.RoleUser, Content: user},
		},
	}, nil
}

// defaultSystemPrompt is the system prompt used when the request
// doesn't override.
const defaultSystemPrompt = `You are a code reviewer. Identify bugs, security issues, performance problems, and maintainability concerns. Be terse. Output each comment as a separate block separated by a blank line. Each comment should include the file path and (when applicable) the line number.`

// summarizeFeedback produces a short text summary of the feedback
// list, truncated by token budget (rough char/4 estimate).
func summarizeFeedback(fbs []Feedback) string {
	if len(fbs) == 0 {
		return ""
	}
	up, down := 0, 0
	var samples []string
	for _, fb := range fbs {
		switch fb.Rating {
		case RatingUp:
			up++
		case RatingDown:
			down++
		}
		if fb.Text != "" && len(samples) < 3 {
			samples = append(samples, fb.Text)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "👍=%d 👎=%d", up, down)
	for _, s := range samples {
		if b.Len()+len(s) > 500 {
			break
		}
		fmt.Fprintf(&b, "\n- %s", s)
	}
	return b.String()
}

// parseLLMComments splits an LLM response body into per-comment
// Comment records. MVP heuristic: each blank-line-separated block
// is one comment; first non-empty line may be of the form
// `<path>:<line>` for line annotations.
func parseLLMComments(filePath, body string) []Comment {
	out := []Comment{}
	var cur strings.Builder
	flush := func() {
		text := strings.TrimSpace(cur.String())
		if text == "" {
			return
		}
		// Look for "path:line:" lead-in.
		fp, line := filePath, 0
		if i := strings.IndexByte(text, ':'); i > 0 && i < 80 {
			head := text[:i]
			// If head looks like a file path, use it; else keep default.
			if strings.Contains(head, "/") || strings.HasSuffix(head, ".go") || strings.HasSuffix(head, ".py") {
				fp = head
				rest := text[i+1:]
				if j := strings.IndexByte(rest, ':'); j > 0 {
					// try to parse a leading number
					if n, err := atoi(rest[:j]); err == nil {
						line = n
						text = strings.TrimSpace(rest[j+1:])
					}
				}
			}
		}
		out = append(out, Comment{
			FilePath:    fp,
			LineNumber:  line,
			Body:        text,
			Fingerprint: Fingerprint(text),
		})
		cur.Reset()
	}
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			flush()
			continue
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	flush()
	return out
}

// atoi is a small inline integer parser; returns error if not all digits.
func atoi(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

// timed wraps a step function with metric + duration tracking.
func timed[T any](step StepName, m *Metrics, fn func() (T, error)) (T, error) {
	t := time.Now()
	out, err := fn()
	if m != nil {
		m.ObserveStep(step, time.Since(t).Seconds())
	}
	return out, err
}
