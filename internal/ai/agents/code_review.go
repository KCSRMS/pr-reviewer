package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Astraxx04/pr-reviewer/internal/ai"
	"github.com/Astraxx04/pr-reviewer/internal/ai/llm"
	"github.com/Astraxx04/pr-reviewer/internal/ai/mcp"
	"github.com/Astraxx04/pr-reviewer/internal/metrics"
)

type CodeReviewAgent struct {
	registry *llm.ProviderRegistry
}

func NewCodeReviewAgent(registry *llm.ProviderRegistry) *CodeReviewAgent {
	return &CodeReviewAgent{registry: registry}
}

func (a *CodeReviewAgent) Process(ctx context.Context, req mcp.Request) (*mcp.Response, error) {
	provider, model, err := a.resolveProvider(req)
	if err != nil {
		return nil, err
	}

	resp, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: reviewSystemPrompt(req.Context, ai.RolePrimary),
		UserPrompt:   req.Query,
		Model:        model,
	})
	if err != nil {
		return nil, fmt.Errorf("code-review agent: %w", err)
	}
	metrics.RecordLLMTokens(model, resp.InputTokens, resp.OutputTokens)

	if err := validateAgentJSON(resp.Content); err != nil {
		return nil, fmt.Errorf("code-review agent: invalid response JSON: %w", err)
	}

	return &mcp.Response{
		Content: resp.Content,
		Metadata: map[string]any{
			"input_tokens":  resp.InputTokens,
			"output_tokens": resp.OutputTokens,
			"provider":      provider.Name(),
		},
	}, nil
}

func (a *CodeReviewAgent) resolveProvider(req mcp.Request) (llm.Provider, string, error) {
	if id, ok := req.Context["provider_id"].(string); ok && id != "" {
		p, model, err := a.registry.Get(id)
		if err != nil {
			return nil, "", err
		}
		if override, ok := req.Context["model"].(string); ok && override != "" {
			model = override
		}
		return p, model, nil
	}
	return a.registry.Default()
}

func validateAgentJSON(s string) error {
	s = stripJSONFence(s)
	var v struct {
		Summary  string `json:"summary"`
		Comments []any  `json:"comments"`
	}
	return json.Unmarshal([]byte(s), &v)
}

// stripJSONFence removes a leading/trailing markdown code fence (``` or ```json)
// that models often wrap JSON in despite being told not to. Mirrors the stripping
// the reviewer does when it later parses the agent's content.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl != -1 {
		s = s[nl+1:] // drop the opening ``` / ```json line
	}
	if end := strings.LastIndex(s, "```"); end != -1 {
		s = s[:end] // drop the closing fence
	}
	return strings.TrimSpace(s)
}
