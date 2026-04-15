package providercore

import (
	"context"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

// Config captures provider-level defaults wired into the core client.
//
// Provider implementations pass a Config to New and may customise it further
// using Option helpers before constructing the client.
type Config struct {
	// ProviderName is the identifier exported via llm.Provider.Name().
	ProviderName string

	// DefaultModel is applied when the caller leaves Request.Model empty.
	DefaultModel string

	// BaseURL is used when llm.Options does not override the base endpoint.
	BaseURL string

	// BasePath overrides the default path for the selected API (e.g. /v1/chat/completions).
	BasePath string

	// APIHint selects which OpenAI-compatible API to target.
	// Required. Use ApiTypeOpenAIChatCompletion, ApiTypeAnthropicMessages, etc.
	APIHint llm.ApiType

	// DefaultHeaders are applied to every request before per-call mutations.
	DefaultHeaders http.Header

	// CostCalculator enriches Usage records with cost data. When not supplied it
	// defaults to usage.Default(). To disable cost entirely, use WithCostCalculator(nil).
	CostCalculator usage.CostCalculator

	// TokenCounter provides pre-request token estimates.
	TokenCounter tokencount.TokenCounter

	// ErrorParser converts non-2xx HTTP responses into provider-specific errors.
	ErrorParser func(statusCode int, body []byte) error

	// RateLimitParser extracts rate-limit data from HTTP responses.
	RateLimitParser func(*http.Response) *llm.RateLimits

	// UsageExtras returns provider-specific metadata attached to Usage records.
	UsageExtras func(*http.Response) map[string]any

	// HeaderFunc returns per-request headers (e.g. Authorization). Called after
	// DefaultHeaders are copied but before MutateRequest.
	HeaderFunc func(ctx context.Context, req *llm.Request) (http.Header, error)

	// MutateRequest runs after all headers are applied, allowing further
	// adjustments (e.g. adding query params).
	MutateRequest func(r *http.Request)

	// ResolveAPIHint overrides APIHint on a per-request basis.
	ResolveAPIHint func(req llm.Request) llm.ApiType

	// ResolveUpstreamProvider sets StreamContext.UpstreamProvider.
	ResolveUpstreamProvider func(req llm.Request) string

	// ResolveCostTargets returns provider+model used for pricing lookups.
	ResolveCostTargets func(req llm.Request) (provider string, model string)

	// PreprocessRequest allows providers to adjust the built request prior to
	// validation and wire conversion. The returned string is the "requested"
	// model used when emitting ModelResolved events. Return an empty string to
	// use the mutated request's Model.
	PreprocessRequest func(req llm.Request) (llm.Request, string, error)

	costCalculatorSet bool
}

// Option mutates a Config before constructing the Client.
type Option func(*Config)

// WithDefaultModel overrides the fallback model applied to empty requests.
func WithDefaultModel(model string) Option {
	return func(cfg *Config) {
		cfg.DefaultModel = model
	}
}

// WithBaseURL sets the default base URL used when llm.Options leaves it empty.
func WithBaseURL(baseURL string) Option {
	return func(cfg *Config) {
		cfg.BaseURL = baseURL
	}
}

// WithBasePath overrides the API path appended to the base URL.
func WithBasePath(path string) Option {
	return func(cfg *Config) {
		cfg.BasePath = path
	}
}

// WithAPIHint sets the API to target for this provider.
func WithAPIHint(api llm.ApiType) Option {
	return func(cfg *Config) {
		cfg.APIHint = api
	}
}

// WithDefaultHeaders defines static headers applied to each request.
func WithDefaultHeaders(headers http.Header) Option {
	return func(cfg *Config) {
		cfg.DefaultHeaders = headers.Clone()
	}
}

// WithCostCalculator sets the calculator used for Usage cost enrichment.
// Pass nil to explicitly disable cost calculation.
func WithCostCalculator(calc usage.CostCalculator) Option {
	return func(cfg *Config) {
		cfg.CostCalculator = calc
		cfg.costCalculatorSet = true
	}
}

// WithTokenCounter configures pre-request token estimation.
func WithTokenCounter(counter tokencount.TokenCounter) Option {
	return func(cfg *Config) {
		cfg.TokenCounter = counter
	}
}

// WithErrorParser sets a custom HTTP error translator.
func WithErrorParser(fn func(int, []byte) error) Option {
	return func(cfg *Config) {
		cfg.ErrorParser = fn
	}
}

// WithRateLimitParser configures rate-limit extraction from responses.
func WithRateLimitParser(fn func(*http.Response) *llm.RateLimits) Option {
	return func(cfg *Config) {
		cfg.RateLimitParser = fn
	}
}

// WithUsageExtras attaches metadata to Usage records.
func WithUsageExtras(fn func(*http.Response) map[string]any) Option {
	return func(cfg *Config) {
		cfg.UsageExtras = fn
	}
}

// WithHeaderFunc sets a callback returning per-request headers.
func WithHeaderFunc(fn func(context.Context, *llm.Request) (http.Header, error)) Option {
	return func(cfg *Config) {
		cfg.HeaderFunc = fn
	}
}

// WithRequestMutator installs a hook run after headers are applied.
func WithRequestMutator(fn func(*http.Request)) Option {
	return func(cfg *Config) {
		cfg.MutateRequest = fn
	}
}

// WithAPIHintResolver overrides API hint selection per request.
func WithAPIHintResolver(fn func(llm.Request) llm.ApiType) Option {
	return func(cfg *Config) {
		cfg.ResolveAPIHint = fn
	}
}

// WithUpstreamResolver sets a per-request upstream provider resolver.
func WithUpstreamResolver(fn func(llm.Request) string) Option {
	return func(cfg *Config) {
		cfg.ResolveUpstreamProvider = fn
	}
}

// WithCostTargetResolver customises pricing lookup identifiers.
func WithCostTargetResolver(fn func(llm.Request) (provider string, model string)) Option {
	return func(cfg *Config) {
		cfg.ResolveCostTargets = fn
	}
}

// WithPreprocessRequest installs a hook to mutate requests before they are
// marshalled to wire format.
func WithPreprocessRequest(fn func(llm.Request) (llm.Request, string, error)) Option {
	return func(cfg *Config) {
		cfg.PreprocessRequest = fn
	}
}
