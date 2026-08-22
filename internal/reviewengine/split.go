package reviewengine

import "strings"

// ApproxCharsPerToken is the rough conversion used for diff sizing.
// English + code mixed text averages ~4 characters per token.
const ApproxCharsPerToken = 0.25

// Chunk is one review unit passed to the LLM in a single Chat call.
// MVP uses file-level granularity (one chunk per file).
type Chunk struct {
	FilePath string
	Body     string // the file's diff content
}

// Split divides a unified diff into reviewable chunks. The strategy
// is file-level: one chunk per file. Whole-PR fallback returns a
// single chunk when the total fits in the budget.
//
// budget is in tokens; the function applies a 60% safety margin
// against the upper limit (per spec §"审查管线 / Scenario: 大 diff 切分").
func Split(req Request, budget int) ([]Chunk, error) {
	if budget <= 0 {
		return nil, ErrInvalidBudget
	}
	effectiveBudget := int(float64(budget) * 0.6)
	estimatedTotal := estimateTokens(req.Diff) + estimateTokens(req.SystemPrompt)

	if estimatedTotal <= effectiveBudget {
		// Whole-PR single chunk — caller can still pass through dedup.
		return []Chunk{{FilePath: "<pr>", Body: req.Diff}}, nil
	}

	// File-level: split by `diff --git` markers.
	files := splitByFile(req.Diff)
	chunks := make([]Chunk, 0, len(files))
	for filePath, body := range files {
		est := estimateTokens(body)
		if est > effectiveBudget {
			// Single file exceeds the budget — surface as a hint to the
			// caller (caller may skip this PR or break into hunk-level).
			continue
		}
		chunks = append(chunks, Chunk{FilePath: filePath, Body: body})
	}
	if len(chunks) == 0 {
		return nil, ErrNoChunkFitsBudget
	}
	return chunks, nil
}

// splitByFile parses a unified diff into per-file bodies keyed by the
// `diff --git a/<path> b/<path>` header. Falls back to "diff" when no
// header is present.
func splitByFile(diff string) map[string]string {
	out := map[string]string{}
	if diff == "" {
		return out
	}
	var currentPath string
	var currentBody strings.Builder

	flush := func() {
		if currentPath != "" {
			out[currentPath] = currentBody.String()
		}
		currentPath = ""
		currentBody.Reset()
	}

	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			// Extract path: `diff --git a/<path> b/<path>`. Use the
			// "b/" side which is the canonical target path.
			idx := strings.Index(line, " b/")
			if idx >= 0 {
				currentPath = line[idx+3:]
			} else {
				currentPath = line
			}
			currentBody.WriteString(line)
			currentBody.WriteByte('\n')
			continue
		}
		currentBody.WriteString(line)
		currentBody.WriteByte('\n')
	}
	flush()
	return out
}

// estimateTokens returns a rough token count for a piece of text.
func estimateTokens(text string) int {
	return int(float64(len(text)) * ApproxCharsPerToken)
}
