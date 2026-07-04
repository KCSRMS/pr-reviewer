package ai

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Astraxx04/pr-reviewer/internal/github"
	"github.com/Astraxx04/pr-reviewer/internal/metrics"
	"github.com/Astraxx04/pr-reviewer/pkg/logger"
)

const (
	// maxSuggestionLines caps the size of the replacement text itself.
	maxSuggestionLines = 40
	// maxSuggestionRangeLines caps how many diff lines a suggestion may replace.
	maxSuggestionRangeLines = 20
)

// hunk is one @@ block of a unified diff patch: the set of new-file ("RIGHT"
// side) line numbers it covers, mapped to their content (diff marker stripped).
// Deletion lines consume no new-file line number and are therefore absent from
// this map — which is exactly the "not a deleted line" check a suggestion needs.
type hunk struct {
	lines map[int]string
}

var hunkHeaderRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parseHunks extracts the new-file line map for every hunk in a GitHub patch.
func parseHunks(patch string) []hunk {
	var hunks []hunk
	var cur *hunk
	newLine := 0
	for _, line := range strings.Split(patch, "\n") {
		if m := hunkHeaderRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			newLine = n
			hunks = append(hunks, hunk{lines: map[int]string{}})
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil || line == "" {
			continue
		}
		switch line[0] {
		case '+':
			cur.lines[newLine] = line[1:]
			newLine++
		case '-':
			// Deletion: exists only on the old side, consumes no new-side line number.
		case '\\':
			// "\ No newline at end of file" — not a content line.
		default:
			content := line
			if line[0] == ' ' {
				content = line[1:]
			}
			cur.lines[newLine] = content
			newLine++
		}
	}
	return hunks
}

// hunkIndexOf returns the index of the hunk containing line, or -1.
func hunkIndexOf(hunks []hunk, line int) int {
	for i, h := range hunks {
		if _, ok := h.lines[line]; ok {
			return i
		}
	}
	return -1
}

// ValidateSuggestions strips the Suggestion/StartLine fields from any comment
// whose proposed fix can't be safely rendered as a GitHub suggestion block. This
// must run before posting: GitHub's CreateReview rejects the *entire* review
// (422) if a single draft comment's suggestion range touches a line outside the
// diff, a deleted line, or spans more than one hunk. The comment body itself is
// left intact — only the machine-applyable fix is dropped.
func ValidateSuggestions(log *logger.Logger, comments []github.ReviewComment, diff []github.FileDiff) []github.ReviewComment {
	hunksByFile := make(map[string][]hunk, len(diff))
	for _, f := range diff {
		if f.Patch != "" {
			hunksByFile[f.Filename] = parseHunks(f.Patch)
		}
	}

	out := make([]github.ReviewComment, len(comments))
	copy(out, comments)
	for i := range out {
		c := &out[i]
		if c.Suggestion == "" {
			continue
		}
		if reason := validateSuggestion(*c, hunksByFile[c.Path]); reason != "" {
			if log != nil {
				log.Info("suggestion dropped", "path", c.Path, "line", c.Line, "reason", reason)
			}
			metrics.SuggestionsDroppedTotal.WithLabelValues(reason).Inc()
			c.Suggestion = ""
			c.StartLine = 0
			continue
		}
		metrics.SuggestionsEmittedTotal.Inc()
	}
	return out
}

func validateSuggestion(c github.ReviewComment, hunks []hunk) string {
	if c.Side == "LEFT" {
		return "left_side_unsupported"
	}
	if len(hunks) == 0 {
		return "file_not_in_diff"
	}

	startLine := c.StartLine
	if startLine == 0 {
		startLine = c.Line
	}
	if startLine > c.Line {
		return "invalid_range"
	}
	if c.Line-startLine+1 > maxSuggestionRangeLines {
		return "range_too_large"
	}
	if strings.Count(c.Suggestion, "\n")+1 > maxSuggestionLines {
		return "suggestion_too_large"
	}

	endIdx := hunkIndexOf(hunks, c.Line)
	if endIdx == -1 {
		return "line_not_in_diff"
	}
	startIdx := hunkIndexOf(hunks, startLine)
	if startIdx != endIdx {
		return "range_crosses_hunk"
	}

	h := hunks[endIdx]
	original := make([]string, 0, c.Line-startLine+1)
	for ln := startLine; ln <= c.Line; ln++ {
		content, ok := h.lines[ln]
		if !ok {
			return "range_crosses_deletion"
		}
		original = append(original, content)
	}
	if strings.Join(original, "\n") == c.Suggestion {
		return "no_op_suggestion"
	}
	return ""
}
