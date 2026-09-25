package ai

import (
	"strings"
	"testing"
)

func TestComposeSystemPromptUsesDefault(t *testing.T) {
	got := ComposeSystemPrompt("  ", RolePrimary)
	if !strings.Contains(got, "prefer YAGNI, then KISS, then DRY") {
		t.Fatal("default policy missing")
	}
	if !strings.Contains(got, OutputContract) {
		t.Fatal("output contract missing")
	}
	if !strings.Contains(got, RolePrimary) {
		t.Fatal("role missing")
	}
	if strings.Contains(got, "gh api") {
		t.Fatal("policy must not tell the model to post the review")
	}
}

func TestComposeSystemPromptKeepsCustomPolicy(t *testing.T) {
	got := ComposeSystemPrompt("Custom policy.", RoleSecurity)
	if !strings.HasPrefix(got, "Custom policy.") {
		t.Fatalf("custom policy dropped: %s", got)
	}
	if !strings.Contains(got, RoleSecurity) || !strings.Contains(got, `"priority": "p0|p2|p3"`) {
		t.Fatal("role or contract missing")
	}
}

func TestReviewPromptIncludesRulesAndComments(t *testing.T) {
	out := ReviewPrompt.Render(map[string]interface{}{
		"Title":            "Fix sync",
		"RepoRules":        "Prefer YAGNI",
		"ExistingComments": "- a.go:1 @dev: already flagged",
		"Diff":             "diff",
		"DiffTruncated":    false,
	})
	for _, want := range []string{"Prefer YAGNI", "already flagged", "diff"} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q:\n%s", want, out)
		}
	}
}

func TestEffectiveReviewPolicy(t *testing.T) {
	if EffectiveReviewPolicy("  ") != DefaultReviewPolicy {
		t.Fatal("blank policy should use the default")
	}
	if EffectiveReviewPolicy("custom") != "custom" {
		t.Fatal("custom policy should be kept")
	}
}
