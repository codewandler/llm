package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/codewandler/llm"
)

const (
	providerName     = "anthropic"
	defaultBaseURL   = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"
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

func (p *Provider) Models() []llm.Model {
	return []llm.Model{
		{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", Provider: providerName},
		{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Provider: providerName},
	}
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	startTime := time.Now()

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

	body, err := BuildRequest(RequestOptions{
		Model:         opts.Model,
		StreamOptions: opts,
	})
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameAnthropic, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameAnthropic, resp.StatusCode, string(errBody))
	}

	pub, ch := llm.NewEventPublisher()
	go ParseStream(ctx, resp.Body, pub, StreamMeta{
		RequestedModel: opts.Model,
		ResolvedModel:  opts.Model,
		StartTime:      startTime,
	})
	return ch, nil
}

func (p *Provider) newAPIRequest(ctx context.Context, apiKey string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}

var _ llm.Provider = (*Provider)(nil)
