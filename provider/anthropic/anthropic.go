package anthropic

import (
	"bytes"
	"context"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/providercore"
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
	inner  *providercore.Provider
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

	p.inner = providercore.NewProvider(providercore.NewOptions(
		providercore.WithProviderName(providerName),
		providercore.WithBaseURL(defaultBaseURL),
		providercore.WithAPIHint(llm.ApiTypeAnthropicMessages),
		providercore.WithModels(allModelsWithAliases),
		providercore.WithDefaultHeaders(http.Header{
			"Accept": {"application/json"},
		}),
		providercore.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			key, err := p.opts.ResolveAPIKey(ctx)
			if err != nil || key == "" {
				return nil, llm.NewErrMissingAPIKey(llm.ProviderNameAnthropic)
			}
			return http.Header{
				"x-api-key": {key},
			}, nil
		}),
		providercore.WithMutateRequest(func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Anthropic-Version", anthropicVersion)
			r.Header.Set("Anthropic-Beta", BetaInterleavedThinking)
		}),
		providercore.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if original != "" {
				if resolved, err := allModelsWithAliases.Resolve(original); err == nil {
					req.Model = resolved.ID
				}
			}
			return req, original, nil
		}),
		providercore.WithMessagesAPITokenCounter(func(ctx context.Context, _ llm.Request, msgReq *providercore.MessagesRequest) (*tokencount.TokenCount, error) {
			count, err := p.CountTokensAPI(ctx, msgReq)
			if err != nil {
				return nil, err
			}
			return &tokencount.TokenCount{InputTokens: count}, nil
		}),
		providercore.WithRateLimitParser(func(resp *http.Response) *llm.RateLimits {
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
		providercore.WithHTTPErrorActionResolver(func(_ llm.Request, statusCode int, _ error) providercore.HTTPErrorAction {
			if llm.IsRetriableHTTPStatus(statusCode) {
				return providercore.HTTPErrorActionReturn
			}
			return providercore.HTTPErrorActionStream
		}),
	), allOpts...)

	return p
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}

func (p *Provider) newAPIRequest(ctx context.Context, apiKey string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("Anthropic-Beta", BetaInterleavedThinking)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}
