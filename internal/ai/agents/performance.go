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

	systemPrompt := reviewSystemPrompt(req.Context, ai.RolePerformance)
	resp, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   req.Query,
		Model:        model,
	})
	if err != nil {
		return nil, fmt.Errorf("performance agent: %w", err)
	}
	metrics.RecordLLMTokens(model, resp.InputTokens, resp.OutputTokens)
	return agentResult("performance", resp.Content, systemPrompt, provider.Name(), resp.InputTokens, resp.OutputTokens, validateAgentJSON(resp.Content))
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
