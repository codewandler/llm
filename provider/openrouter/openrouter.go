package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/providercore"
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
	core         *providercore.Client
	opts         *llm.Options
	client       *http.Client
	defaultModel string
	models       llm.Models
}

// DefaultOptions returns the default options for OpenRouter.
func DefaultOptions() []llm.Option {
	return []llm.Option{llm.WithBaseURL(defaultBaseURL)}
}

// New creates a new OpenRouter provider.
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	llmOpts := llm.Apply(allOpts...)

	client := llmOpts.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	p := &Provider{
		opts:         llmOpts,
		client:       client,
		defaultModel: DefaultModel,
		models:       catalogModels(),
	}
	p.core = p.buildCore()
	return p
}

// WithDefaultModel sets the default model to use when none is specified.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	p.core = p.buildCore()
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

func catalogModels() llm.Models {
	c, err := llm.LoadBuiltInCatalog()
	if err == nil {
		models := llm.CatalogModelsForService(c, providerName, llm.CatalogModelProjectionOptions{
			ProviderName:         providerName,
			ExcludeIntentAliases: true,
		})
		if len(models) > 0 {
			return ensureOpenRouterAliases(models)
		}
	}
	return loadEmbeddedModels()
}

func ensureOpenRouterAliases(models llm.Models) llm.Models {
	aliases := []string{llm.ModelDefault, "auto", llm.ModelFast}
	for i := range models {
		if models[i].ID != "openrouter/auto" {
			continue
		}
		models[i].Aliases = mergeOpenRouterAliases(models[i].Aliases, aliases)
		return models
	}
	return append(llm.Models{{
		ID:       "openrouter/auto",
		Name:     "OpenRouter Auto",
		Provider: providerName,
		Aliases:  aliases,
	}}, models...)
}

func mergeOpenRouterAliases(existing, extra []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	for _, values := range [][]string{existing, extra} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
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

// CreateStream delegates to the shared provider core.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.core.Stream(ctx, src)
}

func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName: providerName,
		DefaultModel: p.defaultModel,
		BaseURL:      defaultBaseURL,
		APIHint:      llm.ApiTypeOpenAIResponses,
	}

	providercore.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
		original := req.Model
		normalized := p.normalizeRequestModel(req.Model)
		req.Model = normalized

		backend, resolved := selectAPI(normalized, req.ApiTypeHint)
		req.ApiTypeHint = resolved
		if backend == orMessages {
			req.Model = strings.TrimPrefix(normalized, "anthropic/")
		}
		return req, original, nil
	})(&cfg)

	providercore.WithAPIHintResolver(func(req llm.Request) llm.ApiType {
		_, hint := selectAPI(req.Model, req.ApiTypeHint)
		return hint
	})(&cfg)

	providercore.WithUpstreamResolver(func(req llm.Request) string {
		if req.ApiTypeHint == llm.ApiTypeAnthropicMessages {
			return "anthropic"
		}
		return upstreamProviderFromModel(req.Model)
	})(&cfg)

	providercore.WithCostTargetResolver(func(req llm.Request) (string, string) {
		if req.ApiTypeHint == llm.ApiTypeAnthropicMessages {
			return "anthropic", req.Model
		}
		upstream := upstreamProviderFromModel(req.Model)
		if upstream == providerName {
			return "", ""
		}
		prefix := upstream + "/"
		model := strings.TrimPrefix(req.Model, prefix)
		return upstream, model
	})(&cfg)

	providercore.WithRequestMutator(func(r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/messages") {
			r.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
			r.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)
		}
	})(&cfg)

	cfg.HeaderFunc = func(ctx context.Context, req *llm.Request) (http.Header, error) {
		key, err := p.opts.ResolveAPIKey(ctx)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, llm.NewErrMissingAPIKey(providerName)
		}
		return http.Header{"Authorization": {"Bearer " + key}}, nil
	}

	cfg.TokenCounter = tokencount.TokenCounterFunc(p.CountTokens)

	return providercore.New(cfg, p.coreOptions()...)
}

func (p *Provider) coreOptions() []llm.Option {
	if p.opts == nil {
		return nil
	}
	var opts []llm.Option
	if p.opts.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(p.opts.BaseURL))
	}
	if p.opts.HTTPClient != nil {
		opts = append(opts, llm.WithHTTPClient(p.opts.HTTPClient))
	}
	if p.opts.APIKeyFunc != nil {
		opts = append(opts, llm.WithAPIKeyFunc(p.opts.APIKeyFunc))
	}
	if p.opts.Logger != nil {
		opts = append(opts, llm.WithLogger(p.opts.Logger))
	}
	return opts
}

// selectAPI decides whether to use the Responses API or the Messages API.
func selectAPI(model string, hint llm.ApiType) (orAPIBackend, llm.ApiType) {
	if hint == llm.ApiTypeAnthropicMessages {
		return orMessages, llm.ApiTypeAnthropicMessages
	}
	if strings.HasPrefix(model, "anthropic/") {
		return orMessages, llm.ApiTypeAnthropicMessages
	}
	return orResponses, llm.ApiTypeOpenAIResponses
}

// upstreamProviderFromModel extracts the provider prefix from an OpenRouter model ID.
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

type orAPIBackend int

const (
	orResponses orAPIBackend = iota
	orMessages
)
