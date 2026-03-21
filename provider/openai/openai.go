package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/codewandler/llm"
)

const (
	defaultBaseURL = "https://api.openai.com"
	providerName   = "openai"

	// DefaultModel is the recommended default model (fast and capable).
	DefaultModel = "gpt-4o-mini"
)

// Provider implements the OpenAI LLM backend.
// It dispatches to the Responses API for Codex models and to Chat Completions
// for everything else.
type Provider struct {
	opts         *llm.Options
	defaultModel string
	client       *http.Client
}

// DefaultOptions returns the default options for the OpenAI provider.
// The API key is read from OPENAI_API_KEY or OPENAI_KEY environment variables.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("OPENAI_API_KEY", "OPENAI_KEY"),
	}
}

// New creates a new OpenAI provider with the given options applied on top of
// DefaultOptions.
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	return &Provider{
		opts:         cfg,
		defaultModel: DefaultModel,
		client:       &http.Client{},
	}
}

// WithDefaultModel returns a copy of the provider using the given default model.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	clone := *p
	clone.defaultModel = modelID
	return &clone
}

// GetDefaultModel returns the configured default model ID.
func (p *Provider) GetDefaultModel() string {
	return p.defaultModel
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return providerName }

// Models returns a curated list of well-known OpenAI models.
func (p *Provider) Models() []llm.Model {
	models := make([]llm.Model, 0, len(modelOrder))
	for _, id := range modelOrder {
		if info, ok := modelRegistry[id]; ok {
			models = append(models, llm.Model{
				ID:       info.ID,
				Name:     info.Name,
				Provider: providerName,
			})
		}
	}
	return models
}

// FetchModels retrieves the live list of models from the OpenAI API.
func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	apiKey, err := p.opts.APIKeyFunc(ctx)
	if err != nil {
		return nil, fmt.Errorf("get API key: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai models API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]llm.Model, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, llm.Model{
			ID:       m.ID,
			Name:     m.ID,
			Provider: providerName,
		})
	}
	return models, nil
}

// Stream dispatches to the Responses API for Codex models, and to Chat
// Completions for everything else.
//
// Reasoning effort is validated and mapped before the request is forwarded.
// Unknown models (not in the registry) default to Chat Completions so that
// newly released non-Codex models work without a registry update.
func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	enriched, err := enrichOpts(opts)
	if err != nil {
		return nil, err
	}

	if isCodexModel(opts.Model) {
		return p.streamResponses(ctx, enriched)
	}
	return p.streamCompletions(ctx, enriched)
}

// enrichOpts resolves model-specific fields before dispatch.
// Currently handles reasoning effort mapping and prompt cache retention.
func enrichOpts(opts llm.StreamOptions) (llm.StreamOptions, error) {
	if opts.ReasoningEffort != "" {
		mapped, err := mapReasoningEffort(opts.Model, opts.ReasoningEffort)
		if err != nil {
			return opts, err
		}
		opts.ReasoningEffort = llm.ReasoningEffort(mapped)
	}

	// Explicit CacheHint TTL "1h" overrides model-based detection.
	if opts.CacheHint != nil && opts.CacheHint.Enabled && opts.CacheHint.TTL == "1h" {
		opts.PromptCacheRetention = "24h"
		return opts, nil
	}

	if info, err := getModelInfo(opts.Model); err == nil && info.SupportsExtendedCache {
		opts.PromptCacheRetention = "24h"
	}

	return opts, nil
}
