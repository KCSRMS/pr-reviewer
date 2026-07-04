package github

import (
	"strings"
	"testing"
)

func TestBuildDraftCommentPlain(t *testing.T) {
	rc := &ReviewComment{Path: "main.go", Line: 10, Side: "RIGHT", Body: "fix this"}
	dc := buildDraftComment(rc)

	if dc.GetBody() != "fix this" {
		t.Errorf("Body = %q, want %q", dc.GetBody(), "fix this")
	}
	if dc.GetLine() != 10 {
		t.Errorf("Line = %d, want 10", dc.GetLine())
	}
	if dc.GetSide() != "RIGHT" {
		t.Errorf("Side = %q, want RIGHT", dc.GetSide())
	}
	if dc.StartLine != nil {
		t.Errorf("StartLine should be nil for a single-line comment, got %v", *dc.StartLine)
	}
}

func TestBuildDraftCommentWithSuggestion(t *testing.T) {
	rc := &ReviewComment{Path: "main.go", Line: 12, Side: "RIGHT", Body: "off by one", Suggestion: "i < n"}
	dc := buildDraftComment(rc)

	want := "off by one\n\n```suggestion\ni < n\n```"
	if dc.GetBody() != want {
		t.Errorf("Body = %q, want %q", dc.GetBody(), want)
	}
	if dc.StartLine != nil {
		t.Errorf("StartLine should be nil for a single-line suggestion, got %v", *dc.StartLine)
	}
}

func TestBuildDraftCommentMultiLineSuggestion(t *testing.T) {
	rc := &ReviewComment{Path: "main.go", Line: 15, StartLine: 12, Side: "RIGHT", Body: "loop bug", Suggestion: "for i := 0; i < n; i++ {"}
	dc := buildDraftComment(rc)

	if dc.StartLine == nil || dc.GetStartLine() != 12 {
		t.Errorf("StartLine = %v, want 12", dc.StartLine)
	}
	if dc.StartSide == nil || dc.GetStartSide() != "RIGHT" {
		t.Errorf("StartSide = %v, want RIGHT", dc.StartSide)
	}
}

func TestBuildDraftCommentDefaultsSideToRight(t *testing.T) {
	rc := &ReviewComment{Path: "main.go", Line: 5, Body: "no side set"}
	dc := buildDraftComment(rc)
	if dc.GetSide() != "RIGHT" {
		t.Errorf("Side = %q, want RIGHT (default)", dc.GetSide())
	}
}

func TestBuildBodyWithCommentsIncludesSuggestion(t *testing.T) {
	review := &ReviewSubmission{
		Body: "overall summary",
		Comments: []ReviewComment{
			{Path: "main.go", Line: 10, Severity: "error", Body: "bug here", Suggestion: "fixed code"},
		},
	}
	body := buildBodyWithComments(review)
	if !strings.Contains(body, "fixed code") {
		t.Errorf("expected fallback body to include the suggestion text, got: %s", body)
	}
}
