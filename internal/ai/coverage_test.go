package ai

import (
	"strings"
	"testing"

	"github.com/Astraxx04/pr-reviewer/internal/github"
)

func TestSummarizedPathMatchesGeneratedFiles(t *testing.T) {
	for _, path := range []string{
		"docs/Routeman.openapi+json.json",
		"web/package-lock.json",
		"vendor/lib/a.go",
		"src/app.min.js",
		"__snapshots__/button.snap",
	} {
		if !isSummarizedPath(path) {
			t.Errorf("%s should be summarized", path)
		}
	}
	if isSummarizedPath("internal/http/handlers/prs.go") {
		t.Fatal("source file should be reviewed")
	}
}

func TestSecurityPathDoesNotMatchAuthor(t *testing.T) {
	if securityPath("internal/author/author.go") {
		t.Fatal("author must not match auth")
	}
	if !securityPath("internal/http/auth/middleware.go") {
		t.Fatal("auth middleware should match")
	}
}

func TestSummarizeMechanicalExtractsPaths(t *testing.T) {
	f := github.FileDiff{
		Filename: "docs/Routeman.openapi+json.json",
		Patch:    "@@ -1 +1 @@\n-  \"/old\": {\n+  \"/orders\": {\n+    \"Order\": {\n",
	}
	got := summarizeMechanical(f)
	if !strings.Contains(got, "/orders") || !strings.Contains(got, "Order") {
		t.Fatalf("summary = %q", got)
	}
	if strings.Contains(got, "\"/old\": {") {
		t.Fatal("raw patch leaked into the summary")
	}
}

func TestPlanReviewCallsSummarizesOpenAPIWithoutSecurity(t *testing.T) {
	patch := "@@ -1,2 +1,3 @@\n" + strings.Repeat("+  \"/orders\": {}\n", 20)
	req := AnalysisRequest{
		Title: "Update spec",
		Diff: []github.FileDiff{{
			Filename:  "docs/Routeman.openapi+json.json",
			Status:    "modified",
			Additions: 20,
			Patch:     patch,
		}},
	}
	calls, files := planReviewCalls(req, "", "", "")
	if len(calls) != 1 || calls[0].Agent != "code-review" {
		t.Fatalf("calls = %#v", calls)
	}
	if strings.Contains(calls[0].Prompt, `"/orders": {}`) {
		t.Fatal("raw openapi patch was sent")
	}
	if !strings.Contains(calls[0].Prompt, "/orders") {
		t.Fatal("structural path missing from prompt")
	}
	if len(files) != 1 || files[0].Class != fileClassSummarize || files[0].PatchIncluded {
		t.Fatalf("trace = %#v", files)
	}
}

func TestPlanReviewCallsChunksLargeSourceAndRoutesSecurity(t *testing.T) {
	big := "@@ -1,1 +1,1 @@\n" + strings.Repeat("+line\n", reviewTokenBudget)
	req := AnalysisRequest{
		Title: "Big change",
		Diff: []github.FileDiff{
			{Filename: "web/button.go", Status: "modified", Patch: big},
			{Filename: "internal/http/auth/handler.go", Status: "modified", Patch: "@@ -1 +1 @@\n+ok\n"},
		},
		RepoConfig: map[string]AgentConfig{"database": {Enabled: true}},
	}
	calls, files := planReviewCalls(req, "", "", "")
	var code, security int
	for _, c := range calls {
		switch c.Agent {
		case "code-review":
			code++
		case "security":
			security++
			if strings.Contains(c.Prompt, "web/button.go") {
				t.Fatal("security prompt included an unrelated file")
			}
		case "database":
			t.Fatal("database agent ran without a matching file")
		}
	}
	if code < 2 {
		t.Fatalf("expected chunked code-review, calls=%d %#v", code, callNames(calls))
	}
	if security != 1 {
		t.Fatalf("security calls = %d", security)
	}
	var handler TraceFile
	for _, f := range files {
		if f.Path == "internal/http/auth/handler.go" {
			handler = f
		}
	}
	if handler.Class != fileClassReview || !handler.PatchIncluded || len(handler.Agents) < 2 {
		t.Fatalf("handler trace = %#v", handler)
	}
}

func TestChunkKeepsSmallDiffTogether(t *testing.T) {
	files := []github.FileDiff{{Filename: "a.go", Patch: "@@ -1 +1 @@\n+a\n"}}
	chunks := chunkByTokens(files, reviewTokenBudget)
	if len(chunks) != 1 || len(chunks[0]) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestDedupeCommentsKeepsHigherPriority(t *testing.T) {
	got := dedupeComments([]github.ReviewComment{
		{Path: "a.go", Line: 1, Side: "RIGHT", Priority: "p3", Body: "low"},
		{Path: "a.go", Line: 1, Side: "RIGHT", Priority: "p0", Body: "high"},
		{Path: "a.go", Line: 2, Side: "RIGHT", Priority: "p2", Body: "other"},
	})
	if len(got) != 2 || got[0].Priority != "p0" {
		t.Fatalf("deduped = %#v", got)
	}
}

func callNames(calls []reviewCall) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}
