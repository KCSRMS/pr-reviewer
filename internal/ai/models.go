package ai

import (
	"github.com/Astraxx04/pr-reviewer/internal/github"
)

// ReviewResult represents the outcome of an AI review.
type ReviewResult struct {
	Comments     []github.ReviewComment
	Summary      string
	Score        int // 0-100 quality score
	InputTokens  int
	OutputTokens int
	Trace        *ReviewTrace
}

// ReviewTrace is the debug record of one review: which files were sent and
// what each agent was asked and answered. Prompt and response text are capped.
type ReviewTrace struct {
	Files         []TraceFile  `json:"files"`
	DiffTruncated bool         `json:"diff_truncated"`
	OmittedFiles  []string     `json:"omitted_files,omitempty"`
	UserPrompt    string       `json:"user_prompt"`
	Agents        []TraceAgent `json:"agents"`
}

// TraceFile describes one file included in the prompt.
type TraceFile struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
	PatchBytes  int    `json:"patch_bytes"`
	PatchSource string `json:"patch_source"`
}

// TraceAgent is one agent's system prompt and raw response.
type TraceAgent struct {
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	Response     string `json:"response,omitempty"`
	Error        string `json:"error,omitempty"`
}

// AgentConfig holds per-agent provider overrides stored in Repository.Config.
type AgentConfig struct {
	ProviderID string `json:"provider_id"`
	Model      string `json:"model"`
	// Enabled opts an optional agent into the review fan-out. It is ignored for
	// core agents (code-review, security), which always run.
	Enabled bool `json:"enabled"`
}

// AnalysisRequest represents the input to the AI reviewer.
type AnalysisRequest struct {
	Diff       []github.FileDiff
	Title      string
	Body       string
	RepoID     uint                   // used to scope RAG retrieval; 0 disables RAG
	RepoConfig map[string]AgentConfig // agent name → provider+model override
	PRContext  interface{}            // broader context (tickets, docs, etc.)

	TicketContext string // formatted Jira ticket summaries for injection into the prompt

	// New fields for Section 8 features:
	FalsePositivePatterns []string // comment bodies previously marked as false positives
	CustomViolations      []string // pre-formatted violations from .pr-reviewer.yml
	DiffTruncated         bool     // true when some files were omitted for max_diff_lines
	OmittedFiles          []string // filenames left out of Diff because of that cap
	PRTemplate            string   // content of .github/pull_request_template.md
	RepoRules             string   // AGENTS.md and copilot instructions, when present
	ExistingComments      string   // inline review comments already on the PR
	ReviewPolicy          string   // editable system policy; empty uses the built-in default
	ConsensusThreshold    int      // 0=disabled; N=require N agents to agree for p2/p3
	AutoFixEnabled        bool     // when true, agents may propose one-click-applicable suggestions
}
