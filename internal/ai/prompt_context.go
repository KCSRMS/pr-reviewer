package ai

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Astraxx04/pr-reviewer/internal/github"
)

const (
	repoRuleRuneCap      = 8000
	existingCommentLimit = 30
	commentBodyRuneCap   = 300
	existingCommentsCap  = 8000
)

var repoRulePaths = []string{
	"AGENTS.md",
	".github/copilot-instructions.md",
}

// GatherPromptExtras loads repository agent rules and inline comments already
// on the pull request. Missing files and GitHub errors are skipped.
func GatherPromptExtras(ctx context.Context, client github.Client, owner, repo string, number int) (rules, existing string) {
	if client == nil {
		return "", ""
	}
	return loadRepoRules(ctx, client, owner, repo), loadExistingComments(ctx, client, owner, repo, number)
}

func loadRepoRules(ctx context.Context, client github.Client, owner, repo string) string {
	var b strings.Builder
	for _, path := range repoRulePaths {
		content, err := client.GetFileContent(ctx, owner, repo, path)
		content = strings.TrimSpace(content)
		if err != nil || content == "" {
			continue
		}
		content = clipRunes(content, repoRuleRuneCap)
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "### %s\n%s", path, content)
	}
	return b.String()
}

func loadExistingComments(ctx context.Context, client github.Client, owner, repo string, number int) string {
	comments, err := client.ListReviewComments(ctx, owner, repo, number)
	if err != nil || len(comments) == 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, c := range comments {
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		if n >= existingCommentLimit {
			break
		}
		body = clipRunes(oneLine(body), commentBodyRuneCap)
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- %s:%d @%s: %s", c.Path, c.Line, c.Author, body)
		n++
		if utf8.RuneCountInString(b.String()) >= existingCommentsCap {
			break
		}
	}
	return clipRunes(b.String(), existingCommentsCap)
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func clipRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "\n…(truncated)"
}
