package ai

import (
	"strings"
	"testing"

	"github.com/Astraxx04/pr-reviewer/internal/github"
)

func TestParseHunks(t *testing.T) {
	patch := strings.Join([]string{
		"@@ -10,3 +10,4 @@ func foo() {",
		" context1",
		"-old line",
		"+new line",
		"+another new line",
		" context2",
	}, "\n")

	hunks := parseHunks(patch)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	h := hunks[0]

	want := map[int]string{
		10: "context1",
		11: "new line",
		12: "another new line",
		13: "context2",
	}
	for line, content := range want {
		got, ok := h.lines[line]
		if !ok {
			t.Errorf("line %d: not found in hunk", line)
			continue
		}
		if got != content {
			t.Errorf("line %d: got %q, want %q", line, got, content)
		}
	}
	if _, ok := h.lines[9]; ok {
		t.Errorf("line 9 should not be present (before hunk)")
	}
}

func TestParseHunksMultiple(t *testing.T) {
	patch := strings.Join([]string{
		"@@ -1,2 +1,2 @@",
		"-a",
		"+b",
		" c",
		"@@ -20,2 +20,2 @@",
		"-d",
		"+e",
		" f",
	}, "\n")

	hunks := parseHunks(patch)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	if _, ok := hunks[0].lines[20]; ok {
		t.Errorf("first hunk should not contain line 20 from second hunk")
	}
	if _, ok := hunks[1].lines[1]; ok {
		t.Errorf("second hunk should not contain line 1 from first hunk")
	}
}

func TestValidateSuggestion(t *testing.T) {
	patch := strings.Join([]string{
		"@@ -10,3 +10,4 @@",
		" context1",
		"-old line",
		"+new line",
		"+another new line",
		" context2",
	}, "\n")
	hunks := parseHunks(patch)

	tests := []struct {
		name       string
		comment    github.ReviewComment
		wantReason string // "" means valid
	}{
		{
			name:       "valid single line replacement",
			comment:    github.ReviewComment{Line: 11, Side: "RIGHT", Suggestion: "fixed line"},
			wantReason: "",
		},
		{
			name:       "valid multi-line replacement",
			comment:    github.ReviewComment{Line: 12, StartLine: 11, Side: "RIGHT", Suggestion: "fixed\nlines"},
			wantReason: "",
		},
		{
			name:       "left side unsupported",
			comment:    github.ReviewComment{Line: 10, Side: "LEFT", Suggestion: "x"},
			wantReason: "left_side_unsupported",
		},
		{
			name:       "line outside diff",
			comment:    github.ReviewComment{Line: 999, Side: "RIGHT", Suggestion: "x"},
			wantReason: "line_not_in_diff",
		},
		{
			name:       "start line after end line",
			comment:    github.ReviewComment{Line: 11, StartLine: 12, Side: "RIGHT", Suggestion: "x"},
			wantReason: "invalid_range",
		},
		{
			name:       "start line before the hunk (range crosses hunk boundary)",
			comment:    github.ReviewComment{Line: 13, StartLine: 9, Side: "RIGHT", Suggestion: "x"},
			wantReason: "range_crosses_hunk",
		},
		{
			name:       "no-op suggestion",
			comment:    github.ReviewComment{Line: 10, Side: "RIGHT", Suggestion: "context1"},
			wantReason: "no_op_suggestion",
		},
		{
			name:       "range too large",
			comment:    github.ReviewComment{Line: 1050, StartLine: 1000, Side: "RIGHT", Suggestion: "x"},
			wantReason: "range_too_large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateSuggestion(tt.comment, hunks)
			if got != tt.wantReason {
				t.Errorf("validateSuggestion() = %q, want %q", got, tt.wantReason)
			}
		})
	}
}

func TestValidateSuggestionRangeCrossesHunk(t *testing.T) {
	patch := strings.Join([]string{
		"@@ -1,2 +1,2 @@",
		" a",
		" b",
		"@@ -20,2 +20,2 @@",
		" c",
		" d",
	}, "\n")
	hunks := parseHunks(patch)

	// line=20 is in the second hunk, start_line=2 is in the first — different hunks.
	c := github.ReviewComment{Line: 20, StartLine: 2, Side: "RIGHT", Suggestion: "x"}
	if got := validateSuggestion(c, hunks); got != "range_crosses_hunk" {
		t.Errorf("validateSuggestion() = %q, want range_crosses_hunk", got)
	}
}

func TestValidateSuggestions_StripsInvalidKeepsComment(t *testing.T) {
	diff := []github.FileDiff{
		{
			Filename: "main.go",
			Patch: strings.Join([]string{
				"@@ -10,3 +10,4 @@",
				" context1",
				"-old line",
				"+new line",
				"+another new line",
				" context2",
			}, "\n"),
		},
	}

	comments := []github.ReviewComment{
		{Path: "main.go", Line: 11, Side: "RIGHT", Body: "fix this", Suggestion: "good fix"},
		{Path: "main.go", Line: 999, Side: "RIGHT", Body: "unfixable", Suggestion: "bad fix"},
		{Path: "other.go", Line: 5, Side: "RIGHT", Body: "no diff for this file", Suggestion: "x"},
	}

	out := ValidateSuggestions(nil, comments, diff)
	if len(out) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(out))
	}
	if out[0].Suggestion != "good fix" {
		t.Errorf("valid suggestion should survive, got %q", out[0].Suggestion)
	}
	if out[1].Suggestion != "" || out[1].Body != "unfixable" {
		t.Errorf("invalid suggestion should be stripped but comment kept: %+v", out[1])
	}
	if out[2].Suggestion != "" || out[2].Body != "no diff for this file" {
		t.Errorf("suggestion for file not in diff should be stripped but comment kept: %+v", out[2])
	}
}
