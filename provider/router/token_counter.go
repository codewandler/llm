package router

import (
	"context"
	"fmt"

	"github.com/codewandler/llm"
)

// CountTokens implements llm.TokenCounter by delegating to the first resolved
// target provider for the given model alias or ID.
//
// The model alias is resolved using the same alias map as CreateStream, and the
// underlying provider's native model ID is used for encoding selection. If the
// resolved provider does not implement llm.TokenCounter, an error is returned.
func (p *Provider) CountTokens(ctx context.Context, req llm.TokenCountRequest) (*llm.TokenCount, error) {
	if req.Model == "" {
		return nil, fmt.Errorf("router: CountTokens: model is required")
	}

	targets, ok := p.aliasMap[req.Model]
	if !ok || len(targets) == 0 {
		return nil, fmt.Errorf("router: CountTokens: %w: %s", ErrUnknownModel, req.Model)
	}

	target := targets[0]
	tc, ok := target.provider.(llm.TokenCounter)
	if !ok {
		return nil, fmt.Errorf("router: CountTokens: provider %q does not implement TokenCounter", target.providerName)
	}

	// Use the resolved native model ID so the provider picks the right encoding.
	delegateReq := req
	delegateReq.Model = target.modelID
	return tc.CountTokens(ctx, delegateReq)
}
