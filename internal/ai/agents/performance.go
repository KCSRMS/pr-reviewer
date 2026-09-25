package agents

import (
	"context"
	"fmt"

	"github.com/Astraxx04/pr-reviewer/internal/ai"
	"github.com/Astraxx04/pr-reviewer/internal/ai/llm"
	"github.com/Astraxx04/pr-reviewer/internal/ai/mcp"
	"github.com/Astraxx04/pr-reviewer/internal/metrics"
)

type PerformanceAgent struct {
	registry *llm.ProviderRegistry
}

func NewPerformanceAgent(registry *llm.ProviderRegistry) *PerformanceAgent {
	return &PerformanceAgent{registry: registry}
}

func (a *PerformanceAgent) Process(ctx context.Context, req mcp.Request) (*mcp.Response, error) {
	provider, model, err := a.resolveProvider(req)
	if err != nil {
		return nil, err
	}

	resp, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: reviewSystemPrompt(req.Context, ai.RolePerformance),
		UserPrompt:   req.Query,
		Model:        model,
	})
	if err != nil {
		return nil, fmt.Errorf("performance agent: %w", err)
	}
	metrics.RecordLLMTokens(model, resp.InputTokens, resp.OutputTokens)

	if err := validateAgentJSON(resp.Content); err != nil {
		return nil, fmt.Errorf("performance agent: invalid response JSON: %w", err)
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

func (a *PerformanceAgent) resolveProvider(req mcp.Request) (llm.Provider, string, error) {
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
