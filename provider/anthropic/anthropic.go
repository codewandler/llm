package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
)

const (
	providerName   = "anthropic"
	defaultBaseURL = "https://api.anthropic.com"
	// AnthropicVersion is the Anthropic API version header value used by all
	// providers that speak the Anthropic API (anthropic, claude).
	AnthropicVersion = "2023-06-01"

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

func (p *Provider) Resolve(modelID string) (llm.Model, error) {
	return allModelsWithAliases.Resolve(modelID)
}
func (p *Provider) CreateStream(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	// Resolve aliases (e.g. "default", "fast") to real model IDs.
	// Unknown model IDs pass through to the API unchanged.
	if opts.Model != "" {
		if resolved, err := p.Resolve(opts.Model); err == nil {
			opts.Model = resolved.ID
		}
	}

	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameAnthropic)
	}
	if apiKey == "" {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameAnthropic)
	}

	apiReq, err := BuildRequest(RequestOptions{
		LLMRequest: opts,
	})
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	body, err := json.MarshalIndent(apiReq, "", "  ")
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	parseOpts := ParseOpts{
		Model:         opts.Model,
		LLMRequest:    &opts,
		RequestParams: apiReq.ControlParams(),
	}

	// Create publisher and emit RequestParamsEvent BEFORE the HTTP call
	// so consumers see what was requested even if the call fails.
	pub, ch := llm.NewEventPublisher()
	PublishRequestParams(pub, parseOpts)

	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(llm.ProviderNameAnthropic, err)
	}

	if resp.StatusCode != http.StatusOK {
		pub.Close()
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameAnthropic, resp.StatusCode, string(errBody))
	}

	// Extract headers for rate-limit info (lowercase keys)
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}
	parseOpts.ResponseHeaders = headers

	ParseStreamWith(ctx, resp.Body, pub, parseOpts)
	return ch, nil
}

func (p *Provider) newAPIRequest(ctx context.Context, apiKey string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}
