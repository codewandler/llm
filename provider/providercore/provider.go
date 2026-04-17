package providercore

import (
	"context"
	"sync"

	"github.com/codewandler/llm"
)

// Provider implements llm.Provider backed by a providercore Client.
type Provider struct {
	client  *Client
	opts    Options
	llmOpts *llm.Options

	modelsOnce   sync.Once
	cachedModels llm.Models
	modelsErr    error
}

func NewProvider(opts Options, llmOpts ...llm.Option) *Provider {
	if err := opts.Validate(); err != nil {
		panic(err.Error())
	}

	applied := llm.Apply(llmOpts...)
	cfg := optsToClientConfig(opts, applied)
	client := New(cfg, llmOpts...)

	return &Provider{
		client:  client,
		opts:    opts,
		llmOpts: applied,
	}
}

func (p *Provider) Name() string { return p.opts.providerName }

func (p *Provider) Models() llm.Models {
	if p.opts.cacheModels {
		p.modelsOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), defaultModelsTimeout)
			defer cancel()
			p.cachedModels, p.modelsErr = p.opts.modelsFunc(ctx)
		})
		if p.modelsErr != nil || p.cachedModels == nil {
			return llm.Models{}
		}
		return p.cachedModels
	}
	models, err := p.opts.modelsFunc(context.Background())
	if err != nil || models == nil {
		return llm.Models{}
	}
	return models
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.client.Stream(ctx, src)
}

func (p *Provider) Options() *llm.Options { return p.llmOpts }

const defaultModelsTimeout = 5 * 1e9

func optsToClientConfig(o Options, llmOpts *llm.Options) clientConfig {
	baseURL := ""
	if o.resolveBaseURL != nil {
		baseURL = o.resolveBaseURL()
	}

	apiHint := llm.ApiTypeAuto
	if o.resolveAPIHint != nil {
		apiHint = o.resolveAPIHint(llm.Request{})
		if apiHint == llm.ApiTypeAuto {
			apiHint = llm.ApiTypeOpenAIChatCompletion
		}
	}

	cfg := clientConfig{
		ProviderName:                o.providerName,
		BaseURL:                     baseURL,
		BasePath:                    o.basePath,
		APIHint:                     apiHint,
		DefaultHeaders:              o.defaultHeaders,
		HeaderFunc:                  o.headerFunc,
		APITokenCounter:             o.apiTokenCounter,
		MessagesAPITokenCounter:     o.messagesAPITokenCounter,
		ErrorParser:                 o.errorParser,
		ResolveHTTPErrorAction:      o.resolveHTTPErrorAction,
		RateLimitParser:             o.rateLimitParser,
		UsageExtras:                 o.usageExtras,
		MutateRequest:               o.mutateRequest,
		ResolveAPIHint:              o.resolveAPIHint,
		PreprocessRequest:           o.preprocessRequest,
		TransformWireRequest:        o.transformWireRequest,
		MessagesRequestTransform:    o.messagesRequestTransform,
		CompletionsRequestTransform: o.completionsRequestTransform,
		ResponsesRequestTransform:   o.responsesRequestTransform,
	}

	return cfg
}
