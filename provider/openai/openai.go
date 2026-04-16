package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/usage"
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
	providerName string
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
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	return &Provider{
		opts:         cfg,
		defaultModel: DefaultModel,
		client:       client,
	}
}

// WithDefaultModel returns a copy of the provider using the given default model.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	clone := *p
	clone.defaultModel = modelID
	return &clone
}

// WithName returns a copy of the provider that identifies itself with the given
// name in error messages, usage records, and stream events. This allows the
// provider to be reused for OpenAI-compatible APIs that are not OpenAI itself
// (e.g. Docker Model Runner).
func (p *Provider) WithName(name string) *Provider {
	clone := *p
	clone.providerName = name
	return &clone
}

// GetDefaultModel returns the configured default model ID.
func (p *Provider) GetDefaultModel() string { return p.defaultModel }
func (p *Provider) Name() string {
	if p.providerName != "" {
		return p.providerName
	}
	return providerName
}

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

func (p *Provider) Resolve(modelID string) (llm.Model, error) { return p.Models().Resolve(modelID) }

// Models returns the catalog-backed OpenAI model view.
func (p *Provider) Models() llm.Models { return p.catalogModels() }

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
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(body))
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
			Provider: p.Name(),
		})
	}
	return models, nil
}

// Stream dispatches to the Responses API for Codex models and gpt-5.4-series
// models, and to Chat Completions for everything else.
//
// Thought effort is validated and mapped before the request is forwarded.
// Unknown models (not in the registry) default to Chat Completions so that
// newly released models work without a registry update.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}

	enriched, err := EnrichRequest(opts)
	if err != nil {
		return nil, err
	}

	if useResponsesAPI(opts.Model) {
		return p.streamResponses(ctx, enriched)
	}
	return p.streamCompletions(ctx, enriched)
}

// enrichOpts resolves model-specific fields before dispatch.
// Currently handles reasoning effort mapping only; cache retention is
// determined at request-build time by wantsExtendedCache.
func EnrichRequest(opts llm.Request) (llm.Request, error) {
	mapped, err := mapEffortAndThinking(opts.Model, opts.Effort, opts.Thinking)
	if err != nil {
		return opts, err
	}
	opts.Effort = llm.Effort(mapped)
	return opts, nil
}

func enrichOpts(opts llm.Request) (llm.Request, error) { return EnrichRequest(opts) }

// wantsExtendedCache reports whether the request should use 24h prompt cache
// retention. An explicit CacheHint with TTL "1h" takes priority; otherwise
// the model registry is consulted for automatic extended-cache support.
func wantsExtendedCache(opts llm.Request) bool {
	if opts.CacheHint != nil && opts.CacheHint.Enabled && opts.CacheHint.TTL == "1h" {
		return true
	}
	info, err := getModelInfo(opts.Model)
	return err == nil && info.SupportsExtendedCache
}

// wantsExtendedCacheInResponsesAPI reports whether the request should include
// prompt_cache_retention: "24h" in a /v1/responses body.
//
// Codex-category models also route through streamResponses but are dispatched
// to the ChatGPT Codex backend, which does not support prompt_cache_retention.
// Only models with UseResponsesAPI: true (e.g. gpt-5.4 series) support it.
func wantsExtendedCacheInResponsesAPI(opts llm.Request) bool {
	if opts.CacheHint != nil && opts.CacheHint.Enabled && opts.CacheHint.TTL == "1h" {
		return true
	}
	info, err := getModelInfo(opts.Model)
	return err == nil && info.UseResponsesAPI && info.SupportsExtendedCache
}

// mapOpenAIFinishReason converts an OpenAI/OpenRouter finish_reason string to
// a typed StopReason.
func mapOpenAIFinishReason(s string) llm.StopReason {
	switch s {
	case "stop":
		return llm.StopReasonEndTurn
	case "tool_calls":
		return llm.StopReasonToolUse
	case "length":
		return llm.StopReasonMaxTokens
	case "content_filter":
		return llm.StopReasonContentFilter
	default:
		return llm.StopReason(s)
	}
}
