package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const (
	providerName   = "anthropic"
	defaultBaseURL = "https://api.anthropic.com"
	// AnthropicVersion is the Anthropic API version header value used by all
	// providers that speak the Anthropic API (anthropic, claude, minimax).
	AnthropicVersion = "2023-06-01"

	// BetaInterleavedThinking is the Anthropic beta header value that enables
	// interleaved thinking — thinking blocks between text and tool-use blocks
	// within a single assistant turn. Always sent for models that support it;
	// harmless no-op for models that don't.
	BetaInterleavedThinking = "interleaved-thinking-2025-05-14"

	// Keep the unexported alias so existing internal usages compile.
	anthropicVersion = AnthropicVersion
)

// Provider implements the direct Anthropic API backend.
type Provider struct {
	opts   *llm.Options
	client *http.Client
}

// DefaultOptions returns the default options for Anthropic.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("ANTHROPIC_API_KEY"),
	}
}

// New creates a new Anthropic provider.
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	return &Provider{opts: cfg, client: client}
}

func (p *Provider) Name() string { return providerName }

func (p *Provider) Models() llm.Models { return allModelsWithAliases }

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

func (p *Provider) Resolve(modelID string) (llm.Model, error) {
	return allModelsWithAliases.Resolve(modelID)
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.buildCore().Stream(ctx, src)
}

func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName: providerName,
		BaseURL:      defaultBaseURL,
		BasePath:     "/v1/messages",
		APIHint:      llm.ApiTypeAnthropicMessages,
		DefaultHeaders: http.Header{
			"Accept": {"application/json"},
		},
		TokenCounter: tokencount.TokenCounterFunc(p.CountTokens),
		APITokenCounter: func(ctx context.Context, _ llm.Request, wire any) (*tokencount.TokenCount, error) {
			msgReq, ok := wire.(*messages.Request)
			if !ok {
				return nil, fmt.Errorf("unexpected messages payload %T", wire)
			}
			count, err := p.CountTokensAPI(ctx, msgReq)
			if err != nil {
				return nil, err
			}
			return &tokencount.TokenCount{InputTokens: count}, nil
		},
		RateLimitParser: func(resp *http.Response) *llm.RateLimits {
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
		},
		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			key, err := p.opts.ResolveAPIKey(ctx)
			if err != nil || key == "" {
				return nil, llm.NewErrMissingAPIKey(llm.ProviderNameAnthropic)
			}
			return http.Header{
				"x-api-key": {key},
			}, nil
		},
		MutateRequest: func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Anthropic-Version", anthropicVersion)
			r.Header.Set("Anthropic-Beta", BetaInterleavedThinking)
		},
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if original != "" {
				if resolved, err := p.Resolve(original); err == nil {
					req.Model = resolved.ID
				}
			}
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
		llm.WithHTTPClient(p.client),
	)
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
