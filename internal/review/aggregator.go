package review

import (
	"context"

	"github.com/Astraxx04/pr-reviewer/internal/ai"
	"github.com/Astraxx04/pr-reviewer/internal/github"
)

type Aggregator interface {
	Aggregate(ctx context.Context, results []ai.ReviewResult) (*FinalReview, error)
}

type aggregatorImpl struct{}

func NewAggregator() Aggregator {
	return &aggregatorImpl{}
}

// priorityRank returns a numeric rank for comparison (lower = more severe).
func priorityRank(p string) int {
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

func (a *aggregatorImpl) Aggregate(_ context.Context, results []ai.ReviewResult) (*FinalReview, error) {
	// Deduplicate by (path, line): keep only the highest-priority comment per line.
	// This prevents two agents reporting the same issue on the same line with different wording.
	type lineKey struct {
		Path string
		Line int
	}
	best := make(map[lineKey]github.ReviewComment)
	var summaries []string

	for _, r := range results {
		if r.Summary != "" {
			summaries = append(summaries, r.Summary)
		}
		for _, c := range r.Comments {
			k := lineKey{c.Path, c.Line}
			if existing, ok := best[k]; !ok || priorityRank(c.Priority) < priorityRank(existing.Priority) {
				best[k] = c
			}
		}
	}

	// Preserve insertion order for consistent output.
	var comments []github.ReviewComment
	for _, c := range best {
		comments = append(comments, c)
	}

	// Reviews are comments. A High finding must not be posted as an approval.
	// Requesting changes stays a manual choice.
	status := "COMMENT"

	summary := "No issues found."
	if len(summaries) > 0 {
		summary = summaries[0]
		for _, s := range summaries[1:] {
			summary += " | " + s
		}
	}

	return &FinalReview{
		Comments: comments,
		Summary:  summary,
		Status:   status,
	}, nil
}
