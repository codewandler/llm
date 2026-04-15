package minimax

import (
	"context"
	"fmt"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const (
	providerName   = "minimax"
	defaultBaseURL = "https://api.minimax.io/anthropic"
	ModelDefault   = ModelM27
)

// Provider implements the MiniMax LLM backend using the Anthropic-compatible API.
type Provider struct {
	core   *providercore.Client
	opts   *llm.Options
	models llm.Models
}

// Option is a functional option for configuring the MiniMax provider.
type Option func(*Provider)

// DefaultOptions returns the default options for MiniMax.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("MINIMAX_API_KEY"),
	}
}

// New creates a new MiniMax provider.
func New(opts ...Option) *Provider {
	llmOpts := llm.Apply(DefaultOptions()...)
	if llmOpts.HTTPClient == nil {
		llmOpts.HTTPClient = llm.DefaultHttpClient()
	}

	p := &Provider{
		opts: llmOpts,
		models: llm.Models{
			{ID: ModelM27, Name: "MiniMax M2.7", Provider: providerName, Aliases: []string{llm.ModelDefault, llm.ModelFast, "minimax"}},
			{ID: ModelM25, Name: "MiniMax M2.5", Provider: providerName},
			{ID: ModelM21, Name: "MiniMax M2.1", Provider: providerName},
			{ID: ModelM2, Name: "MiniMax M2", Provider: providerName},
		},
	}
	p.rebuildCore()

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithLLMOpts applies llm.Option values (base URL, API key, client, etc.).
func WithLLMOpts(llmOpts ...llm.Option) Option {
	return func(p *Provider) {
		all := append(DefaultOptions(), llmOpts...)
		applied := llm.Apply(all...)
		if applied.HTTPClient == nil {
			applied.HTTPClient = llm.DefaultHttpClient()
		}
		p.opts = applied
		p.rebuildCore()
	}
}

func (p *Provider) Name() string { return providerName }

func (p *Provider) CostCalculator() usage.CostCalculator { return usage.Default() }

func (p *Provider) Models() llm.Models { return p.models }

func (p *Provider) Resolve(model string) (llm.Model, error) { return p.models.Resolve(model) }

// CreateStream delegates to the shared provider core.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.core.Stream(ctx, src)
}

func (p *Provider) rebuildCore() {
	cfg := providercore.Config{
		ProviderName: providerName,
		DefaultModel: "",
		BaseURL:      defaultBaseURL,
		BasePath:     "/v1/messages",
		APIHint:      llm.ApiTypeAnthropicMessages,
		DefaultHeaders: http.Header{
			"Accept": {"application/json"},
		},
		TokenCounter: tokencount.TokenCounterFunc(p.CountTokens),
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if original == "" {
				return req, original, fmt.Errorf("model is required")
			}
			if resolved, err := p.Resolve(original); err == nil {
				req.Model = resolved.ID
			}
			return req, original, nil
		},
		TransformWireRequest: func(api llm.ApiType, wire any) (any, error) {
			if api != llm.ApiTypeAnthropicMessages {
				return wire, nil
			}
			msgReq, ok := wire.(*messages.Request)
			if !ok {
				return nil, fmt.Errorf("unexpected messages payload %T", wire)
			}
			msgReq.Thinking = nil
			return msgReq, nil
		},
		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
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
		},
		MutateRequest: func(r *http.Request) {
			r.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
			r.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)
			r.Header.Set("Content-Type", "application/json")
		},
		ResolveUpstreamProvider: func(llm.Request) string {
			return providerName
		},
	}

	opts := coreOptionsFromOpts(p.opts)
	p.core = providercore.New(cfg, opts...)
}

func coreOptionsFromOpts(o *llm.Options) []llm.Option {
	if o == nil {
		return nil
	}
	var opts []llm.Option
	if o.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(o.BaseURL))
	}
	if o.HTTPClient != nil {
		opts = append(opts, llm.WithHTTPClient(o.HTTPClient))
	}
	if o.APIKeyFunc != nil {
		opts = append(opts, llm.WithAPIKeyFunc(o.APIKeyFunc))
	}
	if o.Logger != nil {
		opts = append(opts, llm.WithLogger(o.Logger))
	}
	return opts
}
