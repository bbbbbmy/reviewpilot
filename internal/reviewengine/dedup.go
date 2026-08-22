package reviewengine

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// Fingerprint computes a stable 16-hex-char (64-bit) hash of a
// comment body after whitespace + case normalization. Two bodies
// that differ only in capitalization or spacing share a fingerprint.
//
// Returns "" for empty input.
func Fingerprint(body string) string {
	norm := normalize(body)
	if norm == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:8])
}

// normalize lowercases, replaces non-alphanumeric runs with '_' so
// tokenize() can split on them. Empty runs collapse.
func normalize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	lastUnderscore := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	if lastUnderscore && b.Len() > 0 {
		// trim trailing underscore for cleanliness
		return b.String()[:b.Len()-1]
	}
	return b.String()
}

// Dedup filters out comments whose fingerprint (or near-fingerprint)
// already appears in `existing`. Comments with no fingerprint are
// kept (caller bug) but flagged.
//
// Similarity heuristic (matches spec §"评论去重" 阈值 0.85):
//   - exact fingerprint match ⇒ skip
//   - normalised body jaccard token overlap ≥ 0.85 ⇒ skip
func Dedup(comments []Comment, existing []ExistingComment) []Comment {
	if len(comments) == 0 {
		return nil
	}
	existingFPs := make(map[string]struct{}, len(existing))
	existingTokens := make([]map[string]struct{}, 0, len(existing))
	for _, e := range existing {
		if e.Fingerprint != "" {
			existingFPs[e.Fingerprint] = struct{}{}
		}
		if e.Body != "" {
			existingTokens = append(existingTokens, tokenize(normalize(e.Body)))
		}
	}

	out := make([]Comment, 0, len(comments))
	seen := map[string]struct{}{} // local dedup within the new batch
	for _, c := range comments {
		fp := c.Fingerprint
		if fp == "" {
			fp = Fingerprint(c.Body)
			c.Fingerprint = fp
		}
		if fp == "" {
			// empty body — skip
			continue
		}
		if _, dup := seen[fp]; dup {
			continue
		}
		if _, dup := existingFPs[fp]; dup {
			continue
		}
		if similarToAny(c.Body, existingTokens, 0.85) {
			continue
		}
		seen[fp] = struct{}{}
		out = append(out, c)
	}
	return out
}

// tokenize splits a normalized string into a token set (split on
// non-alphanumeric runs).
func tokenize(s string) map[string]struct{} {
	out := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out[b.String()] = struct{}{}
			b.Reset()
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// similarToAny reports whether `body` has token jaccard ≥ threshold
// with any of the existing token sets.
func similarToAny(body string, existing []map[string]struct{}, threshold float64) bool {
	if body == "" || len(existing) == 0 {
		return false
	}
	tb := tokenize(normalize(body))
	if len(tb) == 0 {
		return false
	}
	for _, te := range existing {
		// jaccard = |A∩B| / |A∪B|
		var inter, union int
		union = len(te)
		for k := range tb {
			if _, ok := te[k]; ok {
				inter++
			} else {
				union++
			}
		}
		if union == 0 {
			continue
		}
		if float64(inter)/float64(union) >= threshold {
			return true
		}
	}
	return false
}
