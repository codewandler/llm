package anthropic

import (
	"context"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	providercore2 "github.com/codewandler/llm/internal/providercore"
	"github.com/codewandler/llm/tokencount"
)

const (
	providerName            = "anthropic"
	defaultBaseURL          = "https://api.anthropic.com"
	AnthropicVersion        = "2023-06-01"
	BetaInterleavedThinking = "interleaved-thinking-2025-05-14"
	anthropicVersion        = AnthropicVersion
)

type Provider struct {
	inner  *providercore2.Provider
	opts   *llm.Options
	client *http.Client
}

func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("ANTHROPIC_API_KEY"),
	}
}

func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	p := &Provider{opts: cfg, client: client}

	p.inner = providercore2.NewProvider(providercore2.NewOptions(
		providercore2.WithProviderName(providerName),
		providercore2.WithBaseURL(defaultBaseURL),
		providercore2.WithAPIHint(llm.ApiTypeAnthropicMessages),
		providercore2.WithModels(allModelsWithAliases),
		providercore2.WithDefaultHeaders(http.Header{
			"Accept": {"application/json"},
		}),
		providercore2.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			key, err := p.opts.ResolveAPIKey(ctx)
			if err != nil || key == "" {
				return nil, llm.NewErrMissingAPIKey(llm.ProviderNameAnthropic)
			}
			return http.Header{
				"x-api-key": {key},
			}, nil
		}),
		providercore2.WithMutateRequest(func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Anthropic-Version", anthropicVersion)
			r.Header.Set("Anthropic-Beta", BetaInterleavedThinking)
		}),
		providercore2.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if original != "" {
				if resolved, err := allModelsWithAliases.Resolve(original); err == nil {
					req.Model = resolved.ID
				}
			}
			return req, original, nil
		}),
		providercore2.WithMessagesAPITokenCounter(func(ctx context.Context, _ llm.Request, msgReq *providercore2.MessagesRequest) (*tokencount.TokenCount, error) {
			count, err := p.CountTokensAPI(ctx, msgReq)
			if err != nil {
				return nil, err
			}
			return &tokencount.TokenCount{InputTokens: count}, nil
		}),
		providercore2.WithRateLimitParser(func(resp *http.Response) *llm.RateLimits {
			if resp == nil {
				return nil
			}
			headers := make(map[string]string, len(resp.Header))
			for k, values := range resp.Header {
				if len(values) > 0 {
					headers[strings.ToLower(k)] = values[0]
				}
			}
			return llm.ParseRateLimits(headers)
		}),
		providercore2.WithHTTPErrorActionResolver(func(_ llm.Request, statusCode int, _ error) providercore2.HTTPErrorAction {
			if llm.IsRetriableHTTPStatus(statusCode) {
				return providercore2.HTTPErrorActionReturn
			}
			return providercore2.HTTPErrorActionStream
		}),
	), allOpts...)

	return p
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}
