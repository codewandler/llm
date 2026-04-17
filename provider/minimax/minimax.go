package minimax

import (
	"context"
	"fmt"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/internal/models"
	providercore2 "github.com/codewandler/llm/internal/providercore"
	"github.com/codewandler/llm/provider/anthropic"
)

const (
	providerName   = "minimax"
	defaultBaseURL = "https://api.minimax.io/anthropic"
)

type Provider struct {
	inner *providercore2.Provider
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

	p.inner = providercore2.NewProvider(providercore2.NewOptions(
		providercore2.WithProviderName(providerName),
		providercore2.WithBaseURL(defaultBaseURL),
		providercore2.WithAPIHint(llm.ApiTypeAnthropicMessages),
		providercore2.WithModels(allModels),
		providercore2.WithDefaultHeaders(http.Header{
			"Accept": {"application/json"},
		}),
		providercore2.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
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
		providercore2.WithMutateRequest(func(r *http.Request) {
			r.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
			r.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)
			r.Header.Set("Content-Type", "application/json")
		}),
		providercore2.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if original == "" {
				return req, original, fmt.Errorf("model is required")
			}
			if resolved, err := allModels.Resolve(original); err == nil {
				req.Model = resolved.ID
			}
			return req, original, nil
		}),
		providercore2.WithMessagesRequestTransform(func(msgReq *providercore2.MessagesRequest) error {
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
