package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
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
	}
}

// WithDefaultModel returns a copy of the provider using the given default model.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	clone := *p
	clone.defaultModel = modelID
	return &clone
}

func (p *Provider) Name() string { return providerName }

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

func (p *Provider) Resolve(modelID string) (llm.Model, error) { return p.Models().Resolve(modelID) }

// Models returns the catalog-backed OpenAI model view.
func (p *Provider) Models() llm.Models { return p.catalogModels() }

// FetchModels retrieves the live list of models from the OpenAI API.
func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("get API key: %w", err)
	}

	client := p.opts.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
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

// CreateStream dispatches to the Responses API for Codex models and gpt-5.4-series
// models, and to Chat Completions for everything else.
//
// Thought effort is validated and mapped before the request is forwarded.
// Unknown models (not in the registry) default to Chat Completions so that
// newly released models work without a registry update.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.buildCore().Stream(ctx, src)
}

func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName:   providerName,
		DefaultModel:   p.defaultModel,
		BaseURL:        defaultBaseURL,
		APIHint:        llm.ApiTypeOpenAIChatCompletion,
		CostCalculator: usage.Default(),
		TokenCounter:   tokencount.TokenCounterFunc(p.CountTokens),
		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			key, err := p.opts.ResolveAPIKey(ctx)
			if err != nil || key == "" {
				return nil, llm.NewErrMissingAPIKey(providerName)
			}
			return http.Header{"Authorization": {"Bearer " + key}}, nil
		},
		ResolveAPIHint: func(req llm.Request) llm.ApiType {
			if useResponsesAPI(req.Model) {
				return llm.ApiTypeOpenAIResponses
			}
			return llm.ApiTypeOpenAIChatCompletion
		},
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			mapped, err := mapEffortAndThinking(req.Model, req.Effort, req.Thinking)
			if err != nil {
				return req, original, err
			}
			req.Effort = llm.Effort(mapped)
			return req, original, nil
		},
		ResolveHTTPErrorAction: func(_ llm.Request, statusCode int, _ error) providercore.HTTPErrorAction {
			if llm.IsRetriableHTTPStatus(statusCode) {
				return providercore.HTTPErrorActionReturn
			}
			return providercore.HTTPErrorActionStream
		},
	}

	return providercore.New(
		cfg,
		llm.WithBaseURL(p.opts.BaseURL),
		llm.WithHTTPClient(p.opts.HTTPClient),
	)
}

// wantsExtendedCache returns true if the request should use 24h cache retention.
// It checks both the CacheHint TTL and the model's SupportsExtendedCache flag.
func wantsExtendedCache(req llm.Request) bool {
	// Explicit cache hint override: TTL "1h" signals 24h extended retention.
	if req.CacheHint != nil && req.CacheHint.Enabled && req.CacheHint.TTL == "1h" {
		return true
	}

	// Model-based auto-detection: use SupportsExtendedCache.
	info, err := getModelInfo(req.Model)
	if err != nil {
		return false
	}
	return info.SupportsExtendedCache
}

// wantsExtendedCacheInResponsesAPI is like wantsExtendedCache but also
// considers models with UseResponsesAPI: true (e.g. gpt-5.4 series).
// Codex models route to a different backend that does not support
// prompt_cache_retention, so we only set it for non-Codex Responses API models.
func wantsExtendedCacheInResponsesAPI(req llm.Request) bool {
	if !wantsExtendedCache(req) {
		return false
	}
	info, err := getModelInfo(req.Model)
	if err != nil {
		return false
	}
	// Codex models (categoryCodex) use a backend that doesn't support this parameter.
	return info.Category != categoryCodex
}

// enrichOpts validates and maps effort/thinking parameters for the OpenAI API.
// It is used by tests that exercise the legacy request builder path.
// In the providercore pipeline, this logic runs in PreprocessRequest.
func enrichOpts(req llm.Request) (llm.Request, error) {
	out := req
	mapped, err := mapEffortAndThinking(req.Model, req.Effort, req.Thinking)
	if err != nil {
		return req, err
	}
	out.Effort = llm.Effort(mapped)
	return out, nil
}
