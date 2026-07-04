package ai

import "testing"

func TestParseAgentResponseWithSuggestion(t *testing.T) {
	content := `{
		"summary": "looks mostly fine",
		"comments": [
			{
				"path": "main.go",
				"line": 12,
				"side": "RIGHT",
				"body": "off by one",
				"priority": "p1",
				"suggestion": "for i := 0; i < n; i++ {",
				"start_line": 12
			},
			{
				"path": "main.go",
				"line": 20,
				"side": "RIGHT",
				"body": "consider renaming",
				"priority": "p3"
			}
		]
	}`

	parsed, err := parseAgentResponse(content)
	if err != nil {
		t.Fatalf("parseAgentResponse() error = %v", err)
	}
	if len(parsed.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(parsed.Comments))
	}
	if got := parsed.Comments[0].Suggestion; got != "for i := 0; i < n; i++ {" {
		t.Errorf("Comments[0].Suggestion = %q, want the loop fix", got)
	}
	if got := parsed.Comments[0].StartLine; got != 12 {
		t.Errorf("Comments[0].StartLine = %d, want 12", got)
	}
	if got := parsed.Comments[1].Suggestion; got != "" {
		t.Errorf("Comments[1].Suggestion = %q, want empty (field omitted)", got)
	}
}
