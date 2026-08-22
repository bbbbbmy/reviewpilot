package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bbbbbmy/reviewpilot/internal/llmopenai"
	"github.com/bbbbbmy/reviewpilot/internal/reviewengine"
)

// reviewCmd implements `reviewpilot review` for local diff review.
// Example:
//
//	reviewpilot review main..feature --repo bbbbbmy/reviewpilot
//	git diff main..feature | reviewpilot review --stdin --repo owner/repo
var reviewCmd = &cobra.Command{
	Use:   "review [<base>...<head>]",
	Short: "Run a review on a git range or patch from stdin",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runReview,
}

func init() {
	reviewCmd.Flags().StringP("repo", "r", "", "Repository identifier (e.g. bbbbbmy/reviewpilot)")
	reviewCmd.Flags().Bool("stdin", false, "Read the patch from stdin instead of a git range")
	reviewCmd.Flags().StringP("out", "o", "-", "Output destination ('-' for stdout, or a file path)")
	_ = reviewCmd.MarkFlagRequired("repo")
}

// runReview is the CLI entry point.
func runReview(cmd *cobra.Command, args []string) error {
	repoID, _ := cmd.Flags().GetString("repo")
	stdin, _ := cmd.Flags().GetBool("stdin")
	outPath, _ := cmd.Flags().GetString("out")

	if repoID == "" {
		return newExitError("missing --repo", 2)
	}

	diff, err := readDiff(args, stdin)
	if err != nil {
		return newExitError(err.Error(), 3)
	}
	if diff == "" {
		return newExitError("empty diff; nothing to review", 4)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	// Build engine: LLM is required, Platform is nil for CLI mode
	// (we don't post to GitHub from CLI — output goes to --out).
	provider, err := buildLLMProvider()
	if err != nil {
		return newExitError(err.Error(), 5)
	}

	eng, err := reviewengine.NewDefaultEngine(reviewengine.EngineDeps{
		LLM:                 provider,
		Platform:            nil, // CLI mode: skip platform dedup
		Feedback:            reviewengine.NewInMemoryFeedbackStore(),
		Metrics:             nil, // CLI doesn't emit metrics
		MaxInputTokensPerPR: 100_000,
		FeedbackWindowDays:  30,
	})
	if err != nil {
		return newExitError(err.Error(), 6)
	}

	res, err := eng.Run(context.Background(), reviewengine.Request{
		RepoID:   repoID,
		PRNumber: 0, // CLI doesn't track a real PR number
		Diff:     diff,
	})
	if err != nil {
		return newExitError("engine: "+err.Error(), 7)
	}

	if err := writeOutput(outPath, res); err != nil {
		return newExitError(err.Error(), 8)
	}

	if res.Status != reviewengine.StatusDone {
		// Non-fatal — write result but exit non-zero so callers know.
		fmt.Fprintf(os.Stderr, "review finished with status=%s reason=%q\n", res.Status, res.Reason)
		return newExitError("non-done status", 9)
	}
	return nil
}

// readDiff returns the unified diff payload. If stdin is true, reads
// stdin. Otherwise runs `git diff <range>` to obtain the diff.
func readDiff(args []string, stdin bool) (string, error) {
	if stdin {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(b), nil
	}
	if len(args) == 0 {
		return "", fmt.Errorf("provide <base>...<head> range or --stdin")
	}
	rangeSpec := args[0]
	if !strings.Contains(rangeSpec, "..") {
		return "", fmt.Errorf("invalid range %q (expected <base>...<head>)", rangeSpec)
	}
	out, err := exec.Command("git", "diff", rangeSpec).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff %s: %w: %s", rangeSpec, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// writeOutput writes the review result to the configured destination.
// Format: a JSON object on success.
func writeOutput(outPath string, res reviewengine.Result) error {
	var w io.Writer
	if outPath == "-" {
		w = os.Stdout
	} else {
		f, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", outPath, err)
		}
		defer f.Close()
		w = f
	}
	type output struct {
		Status    reviewengine.Status    `json:"status"`
		Reason    string                 `json:"reason,omitempty"`
		Comments  []reviewengine.Comment `json:"comments"`
		TokensIn  int                    `json:"tokens_in"`
		TokensOut int                    `json:"tokens_out"`
	}
	o := output{
		Status:    res.Status,
		Reason:    res.Reason,
		Comments:  res.Comments,
		TokensIn:  res.TokenUsage.TokensIn,
		TokensOut: res.TokenUsage.TokensOut,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(o)
}

// buildLLMProvider constructs an LLMProvider from the
// RP_LLM_OPENAI_API_KEY / RP_LLM_OPENAI_BASE_URL env vars.
func buildLLMProvider() (*llmopenai.OpenAICompatibleProvider, error) {
	key := os.Getenv("RP_LLM_OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("RP_LLM_OPENAI_API_KEY is required")
	}
	base := os.Getenv("RP_LLM_OPENAI_BASE_URL")
	model := os.Getenv("RP_LLM_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	return llmopenai.NewProvider(llmopenai.Config{
		APIKey:  key,
		BaseURL: base,
		Model:   model,
	})
}

// exitError is an error that carries an exit code. main() reads the
// code via errors.As and calls os.Exit.
type exitError struct {
	msg  string
	code int
}

func (e *exitError) Error() string { return e.msg }
func newExitError(msg string, code int) *exitError {
	return &exitError{msg: msg, code: code}
}
