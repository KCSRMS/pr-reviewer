package github

import (
	"strconv"
	"strings"
)

const (
	PatchSourceListFiles   = "list_files"
	PatchSourceRawDiff     = "raw_diff"
	PatchSourceUnavailable = "unavailable"
)

// ParseUnifiedDiff splits a GitHub unified diff into per-file patches.
// Each patch starts at the first @@ hunk so it matches the patch field
// returned by the pulls files API. The map key is the new path (+++ b/...).
func ParseUnifiedDiff(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := map[string]string{}
	var path string
	var hunk []string
	inHunk := false

	flush := func() {
		if path == "" || len(hunk) == 0 {
			return
		}
		if hunk[len(hunk)-1] == "" {
			hunk = hunk[:len(hunk)-1]
		}
		if len(hunk) == 0 {
			return
		}
		out[path] = strings.Join(hunk, "\n")
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			path = ""
			hunk = nil
			inHunk = false
			continue
		}
		if strings.HasPrefix(line, "+++ ") {
			path = pathFromDiffHeader(line)
			continue
		}
		if strings.HasPrefix(line, "@@") {
			inHunk = true
		}
		if inHunk {
			hunk = append(hunk, line)
		}
	}
	flush()
	if len(out) == 0 {
		return nil
	}
	return out
}

func pathFromDiffHeader(line string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
	if rest == "/dev/null" || rest == "" {
		return ""
	}
	if strings.HasPrefix(rest, "\"") {
		if unquoted, err := strconv.Unquote(rest); err == nil {
			rest = unquoted
		}
	}
	if i := strings.IndexByte(rest, '\t'); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimPrefix(rest, "b/")
	return rest
}
