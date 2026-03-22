package anthropic

import (
	"bytes"
	"context"
	"fmt"
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

func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
	startTime := time.Now()

	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic API key is not configured")
	}

	body, err := BuildRequest(RequestOptions{
		Model:         opts.Model,
		StreamOptions: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	stream := llm.NewEventStream()
	go ParseStream(ctx, resp.Body, stream, StreamMeta{
		RequestedModel: opts.Model,
		ResolvedModel:  opts.Model,
		StartTime:      startTime,
	})
	return stream.C(), nil
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
