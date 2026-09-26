package agents

import (
	"context"
	"fmt"

	"github.com/Astraxx04/pr-reviewer/internal/ai"
	"github.com/Astraxx04/pr-reviewer/internal/ai/llm"
	"github.com/Astraxx04/pr-reviewer/internal/ai/mcp"
	"github.com/Astraxx04/pr-reviewer/internal/metrics"
)

type SecurityAgent struct {
	registry *llm.ProviderRegistry
}

func NewSecurityAgent(registry *llm.ProviderRegistry) *SecurityAgent {
	return &SecurityAgent{registry: registry}
}

func (a *SecurityAgent) Process(ctx context.Context, req mcp.Request) (*mcp.Response, error) {
	provider, model, err := a.resolveProvider(req)
	if err != nil {
		return nil, err
	}

	systemPrompt := reviewSystemPrompt(req.Context, ai.RoleSecurity)
	resp, err := provider.Complete(ctx, llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   req.Query,
		Model:        model,
	})
	if err != nil {
		return nil, fmt.Errorf("security agent: %w", err)
	}
	metrics.RecordLLMTokens(model, resp.InputTokens, resp.OutputTokens)
	return agentResult("security", resp.Content, systemPrompt, provider.Name(), resp.InputTokens, resp.OutputTokens, validateAgentJSON(resp.Content))
}

func (a *SecurityAgent) resolveProvider(req mcp.Request) (llm.Provider, string, error) {
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
