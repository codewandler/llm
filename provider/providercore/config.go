package providercore

import (
	"context"
	"fmt"
	"net/http"

	completionsapi "github.com/codewandler/agentapis/api/completions"
	messagesapi "github.com/codewandler/agentapis/api/messages"
	responsesapi "github.com/codewandler/agentapis/api/responses"
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
)

type MessagesRequest = messagesapi.Request
type MessagesMessage = messagesapi.Message
type MessagesSystemBlocks = messagesapi.SystemBlocks
type MessagesToolDefinition = messagesapi.ToolDefinition
type MessagesThinkingConfig = messagesapi.ThinkingConfig
type MessagesCacheControl = messagesapi.CacheControl
type CompletionsRequest = completionsapi.Request
type ResponsesRequest = responsesapi.Request

// HTTPErrorAction describes how providercore should surface a non-2xx API response.
type HTTPErrorAction int

const (
	HTTPErrorActionReturn HTTPErrorAction = iota
	HTTPErrorActionStream
)

// ---------------------------------------------------------------------------
// Option — unified option type, applicable to both clientConfig and Options
// ---------------------------------------------------------------------------

// Option configures a providercore Provider or Client.
type Option struct {
	applyCC func(*clientConfig)
	applyO  func(*Options)
}

func (opt Option) applyToClientConfig(cfg *clientConfig) {
	if opt.applyCC != nil {
		opt.applyCC(cfg)
	}
}

func (opt Option) applyToOptions(o *Options) {
	if opt.applyO != nil {
		opt.applyO(o)
	}
}

// --- Option constructors ---

func WithProviderName(name string) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.ProviderName = name },
		applyO:  func(o *Options) { o.providerName = name },
	}
}

func WithBaseURL(url string) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.BaseURL = url },
		applyO:  func(o *Options) { o.resolveBaseURL = func() string { return url } },
	}
}

func WithBaseURLFunc(fn func() string) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.BaseURL = fn() },
		applyO:  func(o *Options) { o.resolveBaseURL = fn },
	}
}

func WithAPIHint(hint llm.ApiType) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.APIHint = hint },
		applyO: func(o *Options) {
			o.resolveAPIHint = func(_ llm.Request) llm.ApiType { return hint }
		},
	}
}

func WithAPIHintResolver(fn func(req llm.Request) llm.ApiType) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.ResolveAPIHint = fn },
		applyO:  func(o *Options) { o.resolveAPIHint = fn },
	}
}

func WithDefaultHeaders(h http.Header) Option {
	clone := h.Clone()
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.DefaultHeaders = clone },
		applyO:  func(o *Options) { o.defaultHeaders = clone },
	}
}

func WithHeaderFunc(fn func(ctx context.Context, req *llm.Request) (http.Header, error)) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.HeaderFunc = fn },
		applyO:  func(o *Options) { o.headerFunc = fn },
	}
}

func WithModels(models llm.Models) Option {
	cp := make(llm.Models, len(models))
	copy(cp, models)
	return Option{
		applyCC: nil,
		applyO:  func(o *Options) { o.modelsFunc = func(_ context.Context) (llm.Models, error) { return cp, nil } },
	}
}

func WithModelsFunc(fn func(ctx context.Context) (llm.Models, error)) Option {
	return Option{
		applyO: func(o *Options) {
			o.modelsFunc = fn
			o.cacheModels = false
		},
	}
}

func WithCachedModelsFunc(fn func(ctx context.Context) (llm.Models, error)) Option {
	return Option{
		applyO: func(o *Options) {
			o.modelsFunc = fn
			o.cacheModels = true
		},
	}
}

func WithPreprocessRequest(fn func(llm.Request) (llm.Request, string, error)) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.PreprocessRequest = fn },
		applyO:  func(o *Options) { o.preprocessRequest = fn },
	}
}

func WithTransformWireRequest(fn func(llm.ApiType, any) (any, error)) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.TransformWireRequest = fn },
		applyO:  func(o *Options) { o.transformWireRequest = fn },
	}
}

func WithMessagesRequestTransform(fn func(*MessagesRequest) error) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.MessagesRequestTransform = fn },
		applyO:  func(o *Options) { o.messagesRequestTransform = fn },
	}
}

func WithCompletionsRequestTransform(fn func(*CompletionsRequest) error) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.CompletionsRequestTransform = fn },
		applyO:  func(o *Options) { o.completionsRequestTransform = fn },
	}
}

func WithResponsesRequestTransform(fn func(*ResponsesRequest) error) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.ResponsesRequestTransform = fn },
		applyO:  func(o *Options) { o.responsesRequestTransform = fn },
	}
}

func WithMutateRequest(fn func(*http.Request)) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.MutateRequest = fn },
		applyO:  func(o *Options) { o.mutateRequest = fn },
	}
}

func WithErrorParser(fn func(int, []byte) error) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.ErrorParser = fn },
		applyO:  func(o *Options) { o.errorParser = fn },
	}
}

func WithHTTPErrorActionResolver(fn func(llm.Request, int, error) HTTPErrorAction) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.ResolveHTTPErrorAction = fn },
		applyO:  func(o *Options) { o.resolveHTTPErrorAction = fn },
	}
}

func WithRateLimitParser(fn func(*http.Response) *llm.RateLimits) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.RateLimitParser = fn },
		applyO:  func(o *Options) { o.rateLimitParser = fn },
	}
}

func WithAPITokenCounter(fn func(context.Context, llm.Request, any) (*tokencount.TokenCount, error)) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.APITokenCounter = fn },
		applyO:  func(o *Options) { o.apiTokenCounter = fn },
	}
}

func WithMessagesAPITokenCounter(fn func(context.Context, llm.Request, *MessagesRequest) (*tokencount.TokenCount, error)) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.MessagesAPITokenCounter = fn },
		applyO:  func(o *Options) { o.messagesAPITokenCounter = fn },
	}
}

func WithUsageExtras(fn func(*http.Response) map[string]any) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.UsageExtras = fn },
		applyO:  func(o *Options) { o.usageExtras = fn },
	}
}

func WithBasePath(path string) Option {
	return Option{
		applyCC: func(cfg *clientConfig) { cfg.BasePath = path },
		applyO:  func(o *Options) { o.basePath = path },
	}
}

// ---------------------------------------------------------------------------
// clientConfig — internal wire config for the Client
// ---------------------------------------------------------------------------

type clientConfig struct {
	ProviderName string
	BaseURL      string
	BasePath     string
	APIHint      llm.ApiType

	DefaultHeaders          http.Header
	APITokenCounter         func(ctx context.Context, req llm.Request, wire any) (*tokencount.TokenCount, error)
	MessagesAPITokenCounter func(ctx context.Context, req llm.Request, wire *MessagesRequest) (*tokencount.TokenCount, error)

	ErrorParser            func(statusCode int, body []byte) error
	ResolveHTTPErrorAction func(req llm.Request, statusCode int, apiErr error) HTTPErrorAction
	RateLimitParser        func(*http.Response) *llm.RateLimits
	UsageExtras            func(*http.Response) map[string]any

	HeaderFunc     func(ctx context.Context, req *llm.Request) (http.Header, error)
	MutateRequest  func(r *http.Request)
	ResolveAPIHint func(req llm.Request) llm.ApiType

	PreprocessRequest           func(req llm.Request) (llm.Request, string, error)
	TransformWireRequest        func(api llm.ApiType, wire any) (any, error)
	MessagesRequestTransform    func(*MessagesRequest) error
	CompletionsRequestTransform func(*CompletionsRequest) error
	ResponsesRequestTransform   func(*ResponsesRequest) error
}

func (cfg *clientConfig) ApplyDefaults() {
	if cfg.DefaultHeaders == nil {
		cfg.DefaultHeaders = make(http.Header)
	}
}

func (cfg clientConfig) Validate() error {
	if cfg.ProviderName == "" {
		return fmt.Errorf("providercore: ProviderName must be set")
	}
	if cfg.APIHint == llm.ApiTypeAuto {
		return fmt.Errorf("providercore: APIHint must be a concrete API type")
	}
	return nil
}

func (cfg *clientConfig) ApplyOptions(opts ...Option) {
	for _, opt := range opts {
		opt.applyToClientConfig(cfg)
	}
}

// ---------------------------------------------------------------------------
// Options — the new public API for constructing a providercore.Provider
// ---------------------------------------------------------------------------

type Options struct {
	providerName   string
	resolveBaseURL func() string
	basePath       string
	resolveAPIHint func(req llm.Request) llm.ApiType
	defaultHeaders http.Header
	headerFunc     func(ctx context.Context, req *llm.Request) (http.Header, error)

	modelsFunc  func(ctx context.Context) (llm.Models, error)
	cacheModels bool

	preprocessRequest           func(req llm.Request) (llm.Request, string, error)
	transformWireRequest        func(api llm.ApiType, wire any) (any, error)
	mutateRequest               func(r *http.Request)
	messagesRequestTransform    func(*MessagesRequest) error
	completionsRequestTransform func(*CompletionsRequest) error
	responsesRequestTransform   func(*ResponsesRequest) error

	errorParser            func(statusCode int, body []byte) error
	resolveHTTPErrorAction func(req llm.Request, statusCode int, apiErr error) HTTPErrorAction
	rateLimitParser        func(*http.Response) *llm.RateLimits

	apiTokenCounter         func(ctx context.Context, req llm.Request, wire any) (*tokencount.TokenCount, error)
	messagesAPITokenCounter func(ctx context.Context, req llm.Request, wire *MessagesRequest) (*tokencount.TokenCount, error)
	usageExtras             func(*http.Response) map[string]any
}

func NewOptions(opts ...Option) Options {
	o := Options{}
	for _, opt := range opts {
		opt.applyToOptions(&o)
	}
	return o
}

func (o Options) Validate() error {
	if o.providerName == "" {
		return fmt.Errorf("providercore: WithProviderName is required")
	}
	if o.resolveAPIHint == nil {
		return fmt.Errorf("providercore: WithAPIHint or WithAPIHintResolver is required")
	}
	if o.modelsFunc == nil {
		return fmt.Errorf("providercore: WithModels, WithModelsFunc, or WithCachedModelsFunc is required")
	}
	return nil
}
