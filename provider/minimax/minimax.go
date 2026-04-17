package minimax

import (
	"context"
	"fmt"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/internal/models"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/providercore"
)

const (
	providerName   = "minimax"
	defaultBaseURL = "https://api.minimax.io/anthropic"
)

type Provider struct {
	inner *providercore.Provider
	opts  *llm.Options
}

type Option func(*Provider)

func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("MINIMAX_API_KEY"),
	}
}

var allModels = func() llm.Models {
	c, err := models.LoadBuiltIn()
	if err != nil {
		return nil
	}
	return llm.CatalogModelsForService(c, providerName, llm.CatalogModelProjectionOptions{
		ProviderName:          providerName,
		ExcludeBuiltinAliases: false,
	})
}()

func New(opts ...Option) *Provider {
	p := &Provider{}

	for _, opt := range opts {
		opt(p)
	}

	if p.opts == nil {
		cfg := llm.Apply(DefaultOptions()...)
		if cfg.HTTPClient == nil {
			cfg.HTTPClient = llm.DefaultHttpClient()
		}
		p.opts = cfg
	}

	if allModels == nil {
		panic("minimax: failed to load models from catalog")
	}

	allLLMOpts := append(DefaultOptions(), llm.WithBaseURL(p.opts.BaseURL), llm.WithAPIKeyFunc(p.opts.ResolveAPIKey))
	if p.opts.HTTPClient != nil {
		allLLMOpts = append(allLLMOpts, llm.WithHTTPClient(p.opts.HTTPClient))
	}
	if p.opts.Logger != nil {
		allLLMOpts = append(allLLMOpts, llm.WithLogger(p.opts.Logger))
	}

	p.inner = providercore.NewProvider(providercore.NewOptions(
		providercore.WithProviderName(providerName),
		providercore.WithBaseURL(defaultBaseURL),
		providercore.WithAPIHint(llm.ApiTypeAnthropicMessages),
		providercore.WithModels(allModels),
		providercore.WithDefaultHeaders(http.Header{
			"Accept": {"application/json"},
		}),
		providercore.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			key, err := p.opts.ResolveAPIKey(ctx)
			if err != nil {
				return nil, err
			}
			if key == "" {
				return nil, llm.NewErrMissingAPIKey(providerName)
			}
			return http.Header{
				"Authorization": {"Bearer " + key},
				"x-api-key":     {key},
			}, nil
		}),
		providercore.WithMutateRequest(func(r *http.Request) {
			r.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
			r.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)
			r.Header.Set("Content-Type", "application/json")
		}),
		providercore.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if original == "" {
				return req, original, fmt.Errorf("model is required")
			}
			if resolved, err := allModels.Resolve(original); err == nil {
				req.Model = resolved.ID
			}
			return req, original, nil
		}),
		providercore.WithMessagesRequestTransform(func(msgReq *providercore.MessagesRequest) error {
			msgReq.Thinking = nil
			return nil
		}),
	), allLLMOpts...)

	return p
}

func WithLLMOpts(llmOpts ...llm.Option) Option {
	return func(p *Provider) {
		all := append(DefaultOptions(), llmOpts...)
		applied := llm.Apply(all...)
		if applied.HTTPClient == nil {
			applied.HTTPClient = llm.DefaultHttpClient()
		}
		p.opts = applied
	}
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}
