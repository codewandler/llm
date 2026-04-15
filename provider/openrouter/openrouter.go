package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const (
	defaultBaseURL = "https://openrouter.ai/api"
	providerName   = "openrouter"

	// DefaultModel is the recommended default model for OpenRouter.
	DefaultModel = "auto"
)

// Provider implements the OpenRouter LLM backend.
type Provider struct {
	opts         *llm.Options
	defaultModel string
	client       *http.Client
	models       llm.Models
}

// DefaultOptions returns the default options for OpenRouter.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
	}
}

// New creates a new OpenRouter provider.
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	return &Provider{
		opts:         cfg,
		defaultModel: DefaultModel,
		client:       client,
		models:       loadEmbeddedModels(),
	}
}

// WithDefaultModel sets the default model to use when none is specified.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	return p
}

func (p *Provider) DefaultModel() string { return p.defaultModel }
func (p *Provider) Name() string         { return providerName }
func (p *Provider) Models() llm.Models   { return p.models }

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

func (p *Provider) Resolve(model string) (llm.Model, error) {
	return p.models.Resolve(p.normalizeRequestModel(model))
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	//nolint:errcheck
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOpenRouter, resp.StatusCode, string(body))
	}
	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]llm.Model, len(result.Data))
	for i, m := range result.Data {
		models[i] = llm.Model{ID: m.ID, Name: m.Name, Provider: providerName}
	}
	return models, nil
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}

	requestedModel := opts.Model
	opts.Model = p.normalizeRequestModel(opts.Model)
	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil || apiKey == "" {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameOpenRouter)
	}

	pub, ch := llm.NewEventPublisher()

	if opts.Model != requestedModel {
		pub.ModelResolved(providerName, requestedModel, opts.Model)
	}

	// Token estimates are path-independent; emit before dispatch.
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, providerName, opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	backend, resolvedApiType := selectAPI(opts.Model, opts.ApiTypeHint)
	switch backend {
	case orResponses:
		go p.streamResponses(ctx, opts, resolvedApiType, apiKey, pub)
	case orMessages:
		go p.streamMessages(ctx, opts, resolvedApiType, apiKey, pub)
	}
	return ch, nil
}

// --- API dispatch ---

type orAPIBackend int

const (
	orResponses orAPIBackend = iota
	orMessages
)

// selectAPI decides whether to use the Responses API or the Messages API.
//
// Routing rules:
//   - explicit ApiTypeAnthropicMessages hint     → Messages API
//   - model prefix "anthropic/"                  → Messages API
//   - everything else (including all openai/*)   → Responses API
//
// The Chat Completions API is intentionally not supported.
func selectAPI(model string, hint llm.ApiType) (orAPIBackend, llm.ApiType) {
	if hint == llm.ApiTypeAnthropicMessages {
		return orMessages, llm.ApiTypeAnthropicMessages
	}
	if strings.HasPrefix(model, "anthropic/") {
		return orMessages, llm.ApiTypeAnthropicMessages
	}
	return orResponses, llm.ApiTypeOpenAIResponses
}

// upstreamProviderFromModel extracts the provider prefix from an OpenRouter
// model ID (e.g. "anthropic/claude-opus-4-5" → "anthropic").
// Returns providerName ("openrouter") as fallback when the ID has no slash.
func upstreamProviderFromModel(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return providerName
}

func (p *Provider) normalizeRequestModel(model string) string {
	switch model {
	case "", llm.ModelDefault:
		return p.defaultModel
	default:
		return model
	}
}
