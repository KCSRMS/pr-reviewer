package handlers

import (
	"strings"
	"testing"

	"github.com/Astraxx04/pr-reviewer/internal/db/models"
)

func TestApplySuggestionSingleLine(t *testing.T) {
	content := "package main\n\nfunc add(a, b int) int {\n\treturn a - b\n}\n"
	c := models.ReviewComment{Line: 4, Suggestion: "\treturn a + b"}

	got, err := applySuggestion(content, c)
	if err != nil {
		t.Fatalf("applySuggestion() error = %v", err)
	}
	want := "package main\n\nfunc add(a, b int) int {\n\treturn a + b\n}\n"
	if got != want {
		t.Errorf("applySuggestion() = %q, want %q", got, want)
	}
}

func TestApplySuggestionMultiLine(t *testing.T) {
	content := strings.Join([]string{
		"func loop() {",
		"\tfor i := 0; i <= n; i++ {",
		"\t\tdo(i)",
		"\t}",
		"}",
	}, "\n")
	c := models.ReviewComment{
		StartLine:  2,
		Line:       2,
		Suggestion: "\tfor i := 0; i < n; i++ {",
	}

	got, err := applySuggestion(content, c)
	if err != nil {
		t.Fatalf("applySuggestion() error = %v", err)
	}
	want := strings.Join([]string{
		"func loop() {",
		"\tfor i := 0; i < n; i++ {",
		"\t\tdo(i)",
		"\t}",
		"}",
	}, "\n")
	if got != want {
		t.Errorf("applySuggestion() = %q, want %q", got, want)
	}
}

func TestApplySuggestionMultiLineRange(t *testing.T) {
	content := strings.Join([]string{"a", "b", "c", "d", "e"}, "\n")
	c := models.ReviewComment{StartLine: 2, Line: 4, Suggestion: "x\ny"}

	got, err := applySuggestion(content, c)
	if err != nil {
		t.Fatalf("applySuggestion() error = %v", err)
	}
	want := strings.Join([]string{"a", "x", "y", "e"}, "\n")
	if got != want {
		t.Errorf("applySuggestion() = %q, want %q", got, want)
	}
}

func TestApplySuggestionOutOfRange(t *testing.T) {
	content := "a\nb\nc"
	tests := []struct {
		name string
		c    models.ReviewComment
	}{
		{"line beyond file", models.ReviewComment{Line: 10, Suggestion: "x"}},
		{"start line after end line", models.ReviewComment{StartLine: 3, Line: 2, Suggestion: "x"}},
		{"zero line", models.ReviewComment{Line: 0, Suggestion: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := applySuggestion(content, tt.c); err == nil {
				t.Errorf("applySuggestion() expected error, got nil")
			}
		})
	}
}
