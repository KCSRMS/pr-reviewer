package ai

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Astraxx04/pr-reviewer/internal/github"
)

// reviewTokenBudget is the approximate user-prompt diff budget for one model
// call. Reviews under this stay a single code-review prompt. Larger reviews
// are split by hunk. Tokens are estimated as 4 characters each.
const reviewTokenBudget = 12_000

const (
	fileClassReview    = "review"
	fileClassSummarize = "summarize"
	fileClassIgnore    = "ignore"
)

// reviewCall is one model call in a review. Name is unique in the trace
// (code-review#2 when an agent is chunked). Agent is the orchestrator key.
type reviewCall struct {
	Name   string
	Agent  string
	Prompt string
	Paths  []string
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

func isSummarizedPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	lower := strings.ToLower(path)
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum", "cargo.lock",
		"composer.lock", "gemfile.lock", "poetry.lock", "bun.lock", "bun.lockb",
		"openapi.json", "openapi.yaml", "openapi.yml", "swagger.json", "swagger.yaml", "swagger.yml":
		return true
	}
	if strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") || strings.HasSuffix(base, ".snap") {
		return true
	}
	if strings.Contains(base, ".openapi") {
		return true
	}
	for _, dir := range []string{"vendor/", "node_modules/", "third_party/", "__snapshots__/"} {
		if strings.HasPrefix(lower, dir) || strings.Contains(lower, "/"+dir) {
			return true
		}
	}
	return false
}

func pathWords(path string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(path), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[f] = true
	}
	return set
}

func pathHasWord(path string, words ...string) bool {
	set := pathWords(path)
	for _, w := range words {
		if set[w] {
			return true
		}
	}
	return false
}

func securityPath(path string) bool {
	return pathHasWord(path, "auth", "oauth", "middleware", "crypto", "secret", "secrets", "password", "credential", "credentials", "sql", "handler", "handlers")
}

func databasePath(path string) bool {
	return pathHasWord(path, "migration", "migrations", "sql", "repository", "repositories", "query", "queries")
}

func performancePath(path string) bool {
	return pathHasWord(path, "cache", "pool", "perf", "performance")
}

var (
	summaryPathPattern = regexp.MustCompile(`"(/[^"]+)"`)
	summaryNamePattern = regexp.MustCompile(`"([A-Z][A-Za-z0-9_.]{1,80})"`)
)

// summarizeMechanical describes a generated or mechanical file without its raw patch.
func summarizeMechanical(f github.FileDiff) string {
	var addedPaths, removedPaths, addedNames, removedNames []string
	seen := map[string]bool{}
	add := func(dst *[]string, key, value string) {
		if seen[key] || len(*dst) >= 30 {
			return
		}
		seen[key] = true
		*dst = append(*dst, value)
	}
	for _, line := range strings.Split(f.Patch, "\n") {
		if line == "" || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		sign := line[0]
		if sign != '+' && sign != '-' {
			continue
		}
		body := line[1:]
		if m := summaryPathPattern.FindStringSubmatch(body); m != nil {
			if sign == '+' {
				add(&addedPaths, "+p"+m[1], m[1])
			} else {
				add(&removedPaths, "-p"+m[1], m[1])
			}
		}
		if m := summaryNamePattern.FindStringSubmatch(body); m != nil {
			if sign == '+' {
				add(&addedNames, "+n"+m[1], m[1])
			} else {
				add(&removedNames, "-n"+m[1], m[1])
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Structural summary only (%d bytes of patch omitted).\n", len(f.Patch))
	writeSummaryList(&b, "Added paths", addedPaths)
	writeSummaryList(&b, "Removed paths", removedPaths)
	writeSummaryList(&b, "Added names", addedNames)
	writeSummaryList(&b, "Removed names", removedNames)
	if len(addedPaths)+len(removedPaths)+len(addedNames)+len(removedNames) == 0 {
		b.WriteString("No path or schema keys detected in the patch.\n")
	}
	return b.String()
}

func writeSummaryList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", label, strings.Join(items, ", "))
}

func formatSummaries(files []github.FileDiff, notes map[string]string) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Generated or mechanical files. The raw patch was not sent. Review this structural summary.\n")
	for _, f := range files {
		fmt.Fprintf(&b, "--- %s (%s +%d -%d)\n%s\n", f.Filename, f.Status, f.Additions, f.Deletions, notes[f.Filename])
	}
	return b.String()
}

func chunkByTokens(files []github.FileDiff, budget int) [][]github.FileDiff {
	if len(files) == 0 {
		return nil
	}
	if budget <= 0 {
		budget = reviewTokenBudget
	}
	pieces := splitFilesToPieces(files, budget)
	var chunks [][]github.FileDiff
	var cur []github.FileDiff
	used := 0
	for _, piece := range pieces {
		tokens := estimateTokens(piece.Patch)
		if len(cur) > 0 && used+tokens > budget {
			chunks = append(chunks, cur)
			cur = nil
			used = 0
		}
		cur = append(cur, piece)
		used += tokens
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

func splitFilesToPieces(files []github.FileDiff, budget int) []github.FileDiff {
	var pieces []github.FileDiff
	for _, f := range files {
		hunks := splitHunks(f.Patch)
		if len(hunks) == 0 {
			pieces = append(pieces, f)
			continue
		}
		var buf []string
		bufTokens := 0
		flush := func() {
			if len(buf) == 0 {
				return
			}
			cp := f
			cp.Patch = strings.Join(buf, "\n")
			pieces = append(pieces, cp)
			buf = nil
			bufTokens = 0
		}
		for _, hunk := range hunks {
			tokens := estimateTokens(hunk)
			if bufTokens > 0 && bufTokens+tokens > budget {
				flush()
			}
			buf = append(buf, hunk)
			bufTokens += tokens
			if bufTokens > budget {
				flush()
			}
		}
		flush()
	}
	return pieces
}

func splitHunks(patch string) []string {
	if patch == "" {
		return nil
	}
	var hunks []string
	var cur []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "@@") && len(cur) > 0 {
			hunks = append(hunks, strings.Join(cur, "\n"))
			cur = nil
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		hunks = append(hunks, strings.Join(cur, "\n"))
	}
	return hunks
}

func filterFiles(files []github.FileDiff, match func(string) bool) []github.FileDiff {
	var out []github.FileDiff
	for _, f := range files {
		if match(f.Filename) {
			out = append(out, f)
		}
	}
	return out
}

func pathsOf(files []github.FileDiff) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if seen[f.Filename] {
			continue
		}
		seen[f.Filename] = true
		out = append(out, f.Filename)
	}
	return out
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func renderUserPrompt(req AnalysisRequest, rag, falsePositives, violations, diffText string) string {
	return ReviewPrompt.Render(map[string]interface{}{
		"Title":            req.Title,
		"Body":             req.Body,
		"Diff":             diffText,
		"RAGContext":       rag,
		"TicketContext":    req.TicketContext,
		"PRTemplate":       req.PRTemplate,
		"RepoRules":        req.RepoRules,
		"ExistingComments": req.ExistingComments,
		"FalsePositives":   falsePositives,
		"CustomViolations": violations,
		"DiffTruncated":    false,
		"OmittedFiles":     "",
	})
}

// planReviewCalls classifies the diff and decides which agent prompts to run.
// A reviewable diff under reviewTokenBudget is one code-review call. A larger
// diff is chunked by hunk. Specialists only receive files whose paths match.
func planReviewCalls(req AnalysisRequest, rag, falsePositives, violations string) ([]reviewCall, []TraceFile) {
	var reviewFiles, summarized []github.FileDiff
	notes := map[string]string{}
	for _, f := range req.Diff {
		if isSummarizedPath(f.Filename) {
			summarized = append(summarized, f)
			notes[f.Filename] = summarizeMechanical(f)
		} else {
			reviewFiles = append(reviewFiles, f)
		}
	}
	summaryText := formatSummaries(summarized, notes)
	chunks := chunkByTokens(reviewFiles, reviewTokenBudget)

	var calls []reviewCall
	switch {
	case len(chunks) == 0 && summaryText != "":
		calls = append(calls, reviewCall{
			Name:   "code-review",
			Agent:  "code-review",
			Prompt: renderUserPrompt(req, rag, falsePositives, violations, summaryText),
			Paths:  pathsOf(summarized),
		})
	case len(chunks) == 1:
		body := formatDiff(chunks[0])
		paths := pathsOf(chunks[0])
		if summaryText != "" {
			body = summaryText + "\n" + body
			paths = append(pathsOf(summarized), paths...)
		}
		calls = append(calls, reviewCall{
			Name:   "code-review",
			Agent:  "code-review",
			Prompt: renderUserPrompt(req, rag, falsePositives, violations, body),
			Paths:  paths,
		})
	case len(chunks) > 1:
		for i, chunk := range chunks {
			body := formatDiff(chunk)
			paths := pathsOf(chunk)
			if i == 0 && summaryText != "" {
				body = summaryText + "\n" + body
				paths = append(pathsOf(summarized), paths...)
			}
			calls = append(calls, reviewCall{
				Name:   fmt.Sprintf("code-review#%d", i+1),
				Agent:  "code-review",
				Prompt: renderUserPrompt(req, rag, falsePositives, violations, body),
				Paths:  paths,
			})
		}
	}

	addSpecialist := func(agent string, match func(string) bool, enabled bool) {
		if !enabled {
			return
		}
		matched := filterFiles(reviewFiles, match)
		parts := chunkByTokens(matched, reviewTokenBudget)
		for i, chunk := range parts {
			name := agent
			if len(parts) > 1 {
				name = fmt.Sprintf("%s#%d", agent, i+1)
			}
			calls = append(calls, reviewCall{
				Name:   name,
				Agent:  agent,
				Prompt: renderUserPrompt(req, rag, falsePositives, violations, formatDiff(chunk)),
				Paths:  pathsOf(chunk),
			})
		}
	}
	addSpecialist("security", securityPath, true)
	perfOn := req.RepoConfig["performance"].Enabled
	dbOn := req.RepoConfig["database"].Enabled
	addSpecialist("performance", performancePath, perfOn)
	addSpecialist("database", databasePath, dbOn)

	return calls, buildTraceFiles(req, summarized, calls)
}

func buildTraceFiles(req AnalysisRequest, summarized []github.FileDiff, calls []reviewCall) []TraceFile {
	agentsFor := map[string][]string{}
	for _, call := range calls {
		for _, path := range call.Paths {
			agentsFor[path] = appendUnique(agentsFor[path], call.Name)
		}
	}
	var out []TraceFile
	for _, f := range req.Ignored {
		out = append(out, newTraceFile(f, fileClassIgnore, false, nil))
	}
	summarizedSet := map[string]bool{}
	for _, f := range summarized {
		summarizedSet[f.Filename] = true
		out = append(out, newTraceFile(f, fileClassSummarize, false, agentsFor[f.Filename]))
	}
	for _, f := range req.Diff {
		if summarizedSet[f.Filename] {
			continue
		}
		out = append(out, newTraceFile(f, fileClassReview, f.Patch != "", agentsFor[f.Filename]))
	}
	return out
}

func newTraceFile(f github.FileDiff, class string, patchIncluded bool, agents []string) TraceFile {
	return TraceFile{
		Path:          f.Filename,
		Status:        f.Status,
		Additions:     f.Additions,
		Deletions:     f.Deletions,
		PatchBytes:    len(f.Patch),
		PatchSource:   f.PatchSource,
		Class:         class,
		PatchIncluded: patchIncluded,
		Agents:        agents,
	}
}

func buildMergePrompt(title, body, coverage, partials string) string {
	return fmt.Sprintf(`You are merging partial reviews of one pull request. Do not invent findings and do not repeat the diff.
Write a single summary of merge risk and the main issues.
Return JSON: {"summary":"...","comments":[]}
Leave comments empty. Inline comments from the partial reviews are kept as-is.

Title: %s
Description:
%s

Coverage:
%s

Partial reviews:
%s
`, title, body, coverage, partials)
}

func coverageText(files []TraceFile) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "- %s class=%s patch_sent=%t agents=%s\n", f.Path, f.Class, f.PatchIncluded, strings.Join(f.Agents, ","))
	}
	return b.String()
}

func dedupeComments(comments []github.ReviewComment) []github.ReviewComment {
	type key struct {
		path string
		line int
		side string
	}
	rank := func(p string) int {
		switch p {
		case "p0":
			return 0
		case "p1":
			return 1
		case "p2":
			return 2
		default:
			return 3
		}
	}
	index := map[key]int{}
	var out []github.ReviewComment
	for _, c := range comments {
		k := key{c.Path, c.Line, c.Side}
		if i, ok := index[k]; ok {
			if rank(c.Priority) < rank(out[i].Priority) {
				out[i] = c
			}
			continue
		}
		index[k] = len(out)
		out = append(out, c)
	}
	return out
}
