// Package minimax provides a MiniMax LLM provider using the Anthropic-compatible API.
// MiniMax recommends their Anthropic-compatible endpoint for new integrations.
// Docs: https://platform.minimax.io/docs/api-reference/text-anthropic-api
package minimax

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
)

const (
	providerName   = "minimax"
	defaultBaseURL = "https://api.minimax.io/anthropic"

	// anthropicVersion is the Anthropic API version header required by the
	// MiniMax Anthropic-compatible endpoint.
	anthropicVersion = "2023-06-01"
)

// Provider implements the MiniMax LLM backend via the Anthropic-compatible API.
type Provider struct {
	opts   *llm.Options
	client *http.Client
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
	cfg := llm.Apply(DefaultOptions()...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	p := &Provider{opts: cfg, client: client}

	// Apply functional options
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithLLMOpts adds custom llm.Option configurations.
func WithLLMOpts(llmOpts ...llm.Option) Option {
	return func(p *Provider) {
		allOpts := append(DefaultOptions(), llmOpts...)
		p.opts = llm.Apply(allOpts...)
		// Update HTTP client if provided
		if p.opts.HTTPClient != nil {
			p.client = p.opts.HTTPClient
		}
	}
}

func (p *Provider) Name() string { return providerName }

func (p *Provider) Models() []llm.Model {
	return []llm.Model{
		{ID: ModelM27, Name: "MiniMax M2.7", Provider: providerName},
		{ID: ModelM25, Name: "MiniMax M2.5", Provider: providerName},
		{ID: ModelM21, Name: "MiniMax M2.1", Provider: providerName},
		{ID: ModelM2, Name: "MiniMax M2", Provider: providerName},
	}
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	startTime := time.Now()

	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil || apiKey == "" {
		return nil, llm.NewErrMissingAPIKey(providerName)
	}

	body, err := anthropic.BuildRequest(anthropic.RequestOptions{
		Model:         opts.Model,
		StreamOptions: opts,
	})
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, llm.NewErrRequestFailed(providerName, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
	}

	pub, ch := llm.NewEventPublisher()
	go p.parseStreamWithCost(ctx, resp.Body, pub, opts.Model, startTime)
	return ch, nil
}

func (p *Provider) parseStreamWithCost(ctx context.Context, body io.ReadCloser, pub llm.Publisher, model string, startTime time.Time) {
	defer pub.Close()
	defer body.Close()

	costPub := &costInjector{Publisher: pub, model: model}
	anthropic.ParseStream(ctx, body, costPub, anthropic.StreamMeta{
		RequestedModel: model,
		ResolvedModel:  model,
		StartTime:      startTime,
	})
}

type costInjector struct {
	llm.Publisher
	model string
}

func (c *costInjector) Usage(u llm.Usage) {
	FillCost(c.model, &u)
	c.Publisher.Usage(u)
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
