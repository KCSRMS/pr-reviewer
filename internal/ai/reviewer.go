package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Astraxx04/pr-reviewer/internal/ai/embeddings"
	"github.com/Astraxx04/pr-reviewer/internal/ai/mcp"
	"github.com/Astraxx04/pr-reviewer/internal/ai/rag"
	"github.com/Astraxx04/pr-reviewer/internal/config"
	"github.com/Astraxx04/pr-reviewer/internal/github"
	"github.com/Astraxx04/pr-reviewer/internal/telemetry"
	"github.com/Astraxx04/pr-reviewer/pkg/logger"

	"go.opentelemetry.io/otel/attribute"
)

type Service interface {
	Review(ctx context.Context, req AnalysisRequest) (*ReviewResult, error)
	// Explain provides a detailed explanation for a specific AI review comment.
	Explain(ctx context.Context, commentBody, filePath string) (string, error)
}

type reviewerImpl struct {
	cfg          *config.Config
	log          *logger.Logger
	embedder     embeddings.Embedder
	retriever    rag.Retriever
	orchestrator *AgentOrchestrator
}

func NewReviewer(
	cfg *config.Config,
	log *logger.Logger,
	embedder embeddings.Embedder,
	retriever rag.Retriever,
	orchestrator *AgentOrchestrator,
) Service {
	return &reviewerImpl{
		cfg:          cfg,
		log:          log,
		embedder:     embedder,
		retriever:    retriever,
		orchestrator: orchestrator,
	}
}

type rawResult struct {
	name   string
	parsed *agentJSON
}

type agentJSON struct {
	Summary  string `json:"summary"`
	Comments []struct {
		Path       string `json:"path"`
		Line       int    `json:"line"`
		Side       string `json:"side"`
		Body       string `json:"body"`
		Priority   string `json:"priority"` // p0|p1|p2|p3
		Severity   string `json:"severity"` // legacy fallback
		Suggestion string `json:"suggestion"`
		StartLine  int    `json:"start_line"`
	} `json:"comments"`
}

func (r *reviewerImpl) Review(ctx context.Context, req AnalysisRequest) (*ReviewResult, error) {
	ctx, span := telemetry.Tracer().Start(ctx, "ai.review")
	defer span.End()
	span.SetAttributes(
		attribute.Int("review.diff_files", len(req.Diff)),
		attribute.Bool("review.diff_truncated", req.DiffTruncated),
		attribute.Bool("review.rag_enabled", r.retriever != nil && req.RepoID > 0),
	)
	r.log.Info("Starting AI review")

	// RAG context (unchanged)
	ragContext := ""
	if r.retriever != nil && req.RepoID > 0 {
		query := buildRAGQuery(req)
		docs, err := r.retriever.Retrieve(ctx, req.RepoID, query, 5)
		if err != nil {
			r.log.Error("RAG retrieval failed", "error", err)
		} else if len(docs) > 0 {
			ragContext = formatRAGContext(docs)
			r.log.Info("RAG context retrieved", "docs", len(docs))
		}
	}

	// Format false positives and custom violations for prompt
	fpStr := strings.Join(req.FalsePositivePatterns, "\n")
	violationsStr := strings.Join(req.CustomViolations, "\n")
	calls, traceFileList := planReviewCalls(req, ragContext, fpStr, violationsStr)
	span.SetAttributes(attribute.Int("review.calls", len(calls)))
	if len(calls) == 0 {
		return &ReviewResult{
			Summary: "No reviewable changes.",
			Score:   100,
			Trace: &ReviewTrace{
				Files:         traceFileList,
				DiffTruncated: req.DiffTruncated,
				OmittedFiles:  req.OmittedFiles,
			},
		}, nil
	}

	type result struct {
		name string
		resp *mcp.Response
		err  error
	}
	ch := make(chan result, len(calls))

	var wg sync.WaitGroup
	for _, call := range calls {
		wg.Add(1)
		go func(call reviewCall) {
			defer wg.Done()
			resp, err := r.orchestrator.Dispatch(ctx, call.Agent, mcp.Request{
				Query:   call.Prompt,
				Context: agentContext(req, call.Agent),
			})
			ch <- result{name: call.Name, resp: resp, err: err}
		}(call)
	}
	wg.Wait()
	close(ch)

	// Buffer all agent results
	var rawResults []rawResult
	var combined ReviewResult
	var primarySummary string
	var otherSummaries []string
	var traceAgents []TraceAgent
	codeReviewCalls := 0
	for _, call := range calls {
		if call.Agent == "code-review" {
			codeReviewCalls++
		}
	}

	for res := range ch {
		agentTrace := TraceAgent{Name: res.name}
		if res.resp != nil {
			if prompt, ok := res.resp.Metadata["system_prompt"].(string); ok {
				agentTrace.SystemPrompt = capTraceText(prompt)
			}
			agentTrace.Response = capTraceText(res.resp.Content)
			if in, ok := res.resp.Metadata["input_tokens"].(int); ok {
				combined.InputTokens += in
			}
			if out, ok := res.resp.Metadata["output_tokens"].(int); ok {
				combined.OutputTokens += out
			}
		}
		if res.err != nil {
			agentTrace.Error = res.err.Error()
			traceAgents = append(traceAgents, agentTrace)
			r.log.Error("Agent dispatch failed", "agent", res.name, "error", res.err)
			continue
		}
		traceAgents = append(traceAgents, agentTrace)
		parsed, err := parseAgentResponse(res.resp.Content)
		if err != nil {
			r.log.Error("Failed to parse agent response", "agent", res.name, "error", err)
			continue
		}
		rawResults = append(rawResults, rawResult{name: res.name, parsed: parsed})
		if parsed.Summary == "" {
			continue
		}
		if res.name == "code-review" || strings.HasPrefix(res.name, "code-review#") {
			if primarySummary == "" {
				primarySummary = parsed.Summary
			} else {
				otherSummaries = append(otherSummaries, parsed.Summary)
			}
		} else {
			otherSummaries = append(otherSummaries, parsed.Summary)
		}
	}
	if primarySummary != "" {
		combined.Summary = primarySummary
	} else {
		combined.Summary = strings.Join(otherSummaries, "\n\n")
	}

	// Build consensus counts (only when threshold > 1)
	type lineKey struct {
		Path string
		Line int
	}
	var lineCount map[lineKey]int
	agentsRan := map[string]bool{}
	for _, call := range calls {
		agentsRan[call.Agent] = true
	}
	if req.ConsensusThreshold > 1 && len(agentsRan) >= req.ConsensusThreshold {
		lineCount = map[lineKey]int{}
		for _, raw := range rawResults {
			seen := map[lineKey]bool{}
			for _, c := range raw.parsed.Comments {
				k := lineKey{c.Path, c.Line}
				if !seen[k] {
					seen[k] = true
					lineCount[k]++
				}
			}
		}
	}

	// Merge agent results
	for _, raw := range rawResults {
		for _, c := range raw.parsed.Comments {
			priority := normalisePriority(c.Priority, c.Severity)
			// Apply consensus filter to p2/p3 comments
			if lineCount != nil && (priority == "p2" || priority == "p3") {
				k := lineKey{c.Path, c.Line}
				if lineCount[k] < req.ConsensusThreshold {
					continue
				}
			}
			comment := github.ReviewComment{
				Path:     c.Path,
				Line:     c.Line,
				Side:     sideOrDefault(c.Side),
				Body:     fmt.Sprintf("%s %s", priorityLabel(priority), c.Body),
				Severity: priorityToSeverity(priority),
				Priority: priority,
			}
			if req.AutoFixEnabled && c.Suggestion != "" {
				comment.Suggestion = c.Suggestion
				comment.StartLine = c.StartLine
			}
			combined.Comments = append(combined.Comments, comment)
		}
	}

	combined.Comments = dedupeComments(combined.Comments)
	if req.AutoFixEnabled {
		combined.Comments = ValidateSuggestions(r.log, combined.Comments, req.Diff)
	}

	combined.Score = computeScore(combined.Comments)
	suggestionCount := 0
	for _, c := range combined.Comments {
		if c.Suggestion != "" {
			suggestionCount++
		}
	}
	var promptLog strings.Builder
	for _, call := range calls {
		fmt.Fprintf(&promptLog, "===== %s =====\n%s\n\n", call.Name, call.Prompt)
	}
	if codeReviewCalls > 1 {
		merged, mergePrompt, mergeTrace, in, out := r.mergeChunkSummaries(ctx, req, traceFileList, rawResults)
		promptLog.WriteString("===== merge =====\n")
		promptLog.WriteString(mergePrompt)
		if mergeTrace != nil {
			traceAgents = append(traceAgents, *mergeTrace)
		}
		combined.InputTokens += in
		combined.OutputTokens += out
		if merged != "" {
			combined.Summary = merged
		} else {
			parts := make([]string, 0, 1+len(otherSummaries))
			if primarySummary != "" {
				parts = append(parts, primarySummary)
			}
			parts = append(parts, otherSummaries...)
			if len(parts) > 0 {
				combined.Summary = strings.Join(parts, "\n\n")
			}
		}
	}

	sort.Slice(traceAgents, func(i, j int) bool { return traceAgents[i].Name < traceAgents[j].Name })
	combined.Trace = &ReviewTrace{
		Files:         traceFileList,
		DiffTruncated: req.DiffTruncated,
		OmittedFiles:  req.OmittedFiles,
		UserPrompt:    capTraceText(promptLog.String()),
		Agents:        traceAgents,
	}

	span.SetAttributes(
		attribute.Int("review.comments", len(combined.Comments)),
		attribute.Int("review.score", combined.Score),
		attribute.Int("review.input_tokens", combined.InputTokens),
		attribute.Int("review.output_tokens", combined.OutputTokens),
		attribute.Int("review.suggestions", suggestionCount),
	)
	r.log.Info("AI review complete", "comments", len(combined.Comments), "score", combined.Score)
	return &combined, nil
}

func (r *reviewerImpl) Explain(ctx context.Context, commentBody, filePath string) (string, error) {
	prompt := fmt.Sprintf(`You are a senior engineer who flagged a code issue during review.

The finding:
%s

File: %s

Provide a detailed explanation covering:
1. Why this specific code pattern is a problem
2. Potential impact (security, correctness, performance, maintainability)
3. A concrete example of how to fix it

Be specific and practical.`, commentBody, filePath)

	resp, err := r.orchestrator.Dispatch(ctx, "code-review", mcp.Request{Query: prompt})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// StripJSONFence removes a leading/trailing markdown code fence (``` or ```json)
// that LLMs often wrap JSON in despite instructions not to.
func StripJSONFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	if nl := strings.IndexByte(content, '\n'); nl != -1 {
		content = content[nl+1:] // drop opening ``` / ```json line
	}
	if end := strings.LastIndex(content, "```"); end != -1 {
		content = content[:end] // drop closing fence
	}
	return strings.TrimSpace(content)
}

func parseAgentResponse(content string) (*agentJSON, error) {
	var out agentJSON
	if err := json.Unmarshal([]byte(StripJSONFence(content)), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func computeScore(comments []github.ReviewComment) int {
	score := 100
	for _, c := range comments {
		switch c.Priority {
		case "p0":
			score -= 25
		case "p1":
			score -= 15
		case "p2":
			score -= 5
		case "p3":
			score -= 1
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

// normalisePriority returns a canonical p0–p3 value.
// If the LLM returned a legacy severity instead, it maps up.
func normalisePriority(priority, severity string) string {
	switch priority {
	case "p0", "p1", "p2", "p3":
		return priority
	}
	// fallback: map old severity field
	switch severity {
	case "error":
		return "p1"
	case "warning":
		return "p2"
	default:
		return "p3"
	}
}

func priorityLabel(p string) string {
	switch p {
	case "p0":
		return "**[High]**"
	case "p1":
		return "**[High]**"
	case "p2":
		return "**[Medium]**"
	default:
		return "**[Low]**"
	}
}

func priorityToSeverity(p string) string {
	switch p {
	case "p0", "p1":
		return "error"
	case "p2":
		return "warning"
	default:
		return "info"
	}
}

func sideOrDefault(side string) string {
	if side == "LEFT" || side == "RIGHT" {
		return side
	}
	return "RIGHT"
}

func buildRAGQuery(req AnalysisRequest) string {
	files := make([]string, 0, len(req.Diff))
	for _, f := range req.Diff {
		files = append(files, f.Filename)
	}
	return fmt.Sprintf("PR: %s\nFiles: %s\nDescription: %s",
		req.Title, strings.Join(files, ", "), req.Body)
}

func formatRAGContext(docs []rag.Document) string {
	var sb strings.Builder
	for i, d := range docs {
		kind, _ := d.Metadata["kind"].(string)
		if kind == "fix" {
			fmt.Fprintf(&sb, "%d. [Known fix] %s\n", i+1, d.Content)
		} else {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, d.Content)
		}
	}
	return sb.String()
}

func formatDiff(files []github.FileDiff) string {
	var sb strings.Builder
	for _, f := range files {
		fmt.Fprintf(&sb, "--- %s (%s +%d -%d)\n", f.Filename, f.Status, f.Additions, f.Deletions)
		if f.Patch != "" {
			sb.WriteString(f.Patch)
		} else if f.Status != "removed" {
			sb.WriteString("(patch text unavailable; this file changed but its diff was not returned)\n")
		}
		sb.WriteString("\n\n")
	}
	return sb.String()
}

const traceTextCap = 400_000

func capTraceText(s string) string {
	if len(s) <= traceTextCap {
		return s
	}
	return s[:traceTextCap] + "\n\n[truncated]"
}

func agentContext(req AnalysisRequest, agentName string) map[string]interface{} {
	agentCtx := map[string]interface{}{}
	if req.AutoFixEnabled {
		agentCtx["suggestions_enabled"] = true
	}
	if req.ReviewPolicy != "" {
		agentCtx["review_policy"] = req.ReviewPolicy
	}
	if ac, ok := req.RepoConfig[agentName]; ok {
		if ac.ProviderID != "" {
			agentCtx["provider_id"] = ac.ProviderID
		}
		if ac.Model != "" {
			agentCtx["model"] = ac.Model
		}
	}
	return agentCtx
}

// mergeChunkSummaries asks the code-review agent for one summary from the
// partial reviews. The diff is not sent again. Inline comments stay on the
// partial results.
func (r *reviewerImpl) mergeChunkSummaries(ctx context.Context, req AnalysisRequest, files []TraceFile, raw []rawResult) (summary, prompt string, trace *TraceAgent, input, output int) {
	var partials strings.Builder
	for _, item := range raw {
		if item.parsed == nil {
			continue
		}
		fmt.Fprintf(&partials, "### %s\n%s\n", item.name, item.parsed.Summary)
		for _, c := range item.parsed.Comments {
			fmt.Fprintf(&partials, "- %s:%d %s %s\n", c.Path, c.Line, c.Priority, c.Body)
		}
		partials.WriteString("\n")
	}
	prompt = buildMergePrompt(req.Title, req.Body, coverageText(files), partials.String())
	resp, err := r.orchestrator.Dispatch(ctx, "code-review", mcp.Request{
		Query:   prompt,
		Context: agentContext(req, "code-review"),
	})
	agentTrace := TraceAgent{Name: "merge"}
	if resp != nil {
		if sp, ok := resp.Metadata["system_prompt"].(string); ok {
			agentTrace.SystemPrompt = capTraceText(sp)
		}
		agentTrace.Response = capTraceText(resp.Content)
		if in, ok := resp.Metadata["input_tokens"].(int); ok {
			input = in
		}
		if out, ok := resp.Metadata["output_tokens"].(int); ok {
			output = out
		}
	}
	if err != nil {
		agentTrace.Error = err.Error()
		r.log.Error("merge summary failed", "error", err)
		return "", prompt, &agentTrace, input, output
	}
	parsed, err := parseAgentResponse(resp.Content)
	if err != nil {
		agentTrace.Error = err.Error()
		r.log.Error("merge summary parse failed", "error", err)
		return "", prompt, &agentTrace, input, output
	}
	return parsed.Summary, prompt, &agentTrace, input, output
}
