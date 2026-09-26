package github

import "testing"

func TestParseUnifiedDiffSplitsFilesAtHunks(t *testing.T) {
	raw := "" +
		"diff --git a/docs/Routeman.openapi+json.json b/docs/Routeman.openapi+json.json\n" +
		"index 111..222 100644\n" +
		"--- a/docs/Routeman.openapi+json.json\n" +
		"+++ b/docs/Routeman.openapi+json.json\n" +
		"@@ -1,2 +1,3 @@\n" +
		" line\n" +
		"-old\n" +
		"+new\n" +
		"diff --git a/added.txt b/added.txt\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n" +
		"+++ b/added.txt\n" +
		"@@ -0,0 +1 @@\n" +
		"+hello\n"

	got := ParseUnifiedDiff(raw)
	if len(got) != 2 {
		t.Fatalf("files = %d, want 2", len(got))
	}
	patch := got["docs/Routeman.openapi+json.json"]
	if patch == "" || patch[0:2] != "@@" {
		t.Fatalf("patch should start at the hunk, got %q", patch)
	}
	if got["added.txt"] != "@@ -0,0 +1 @@\n+hello" {
		t.Fatalf("added.txt patch = %q", got["added.txt"])
	}
}

func TestParseUnifiedDiffQuotedPath(t *testing.T) {
	raw := "diff --git \"a/my file.go\" \"b/my file.go\"\n" +
		"--- \"a/my file.go\"\n" +
		"+++ \"b/my file.go\"\n" +
		"@@ -1 +1 @@\n" +
		"-a\n" +
		"+b\n"
	got := ParseUnifiedDiff(raw)
	if _, ok := got["my file.go"]; !ok {
		t.Fatalf("missing quoted path, keys: %v", got)
	}
}

func TestParseUnifiedDiffSkipsBinary(t *testing.T) {
	raw := "diff --git a/logo.png b/logo.png\n" +
		"Binary files a/logo.png and b/logo.png differ\n"
	if got := ParseUnifiedDiff(raw); len(got) != 0 {
		t.Fatalf("binary file should have no hunk, got %#v", got)
	}
}
