// Package minimax provides a MiniMax LLM provider using the Anthropic-compatible API.
// MiniMax recommends their Anthropic-compatible endpoint for new integrations.
// Docs: https://platform.minimax.io/docs/api-reference/text-anthropic-api
package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
)

const ModelDefault = ModelM27

const (
	providerName   = "minimax"
	defaultBaseURL = "https://api.minimax.io/anthropic"
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

func (p *Provider) Models() llm.Models {
	return []llm.Model{
		{ID: ModelM27, Name: "MiniMax M2.7", Provider: providerName, Aliases: []string{llm.ModelDefault, llm.ModelFast, "minimax"}},
		{ID: ModelM25, Name: "MiniMax M2.5", Provider: providerName},
		{ID: ModelM21, Name: "MiniMax M2.1", Provider: providerName},
		{ID: ModelM2, Name: "MiniMax M2", Provider: providerName},
	}
}

func (p *Provider) Resolve(model string) (llm.Model, error) {
	return p.Models().Resolve(model)
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	// Resolve aliases (e.g. "default", "fast", "minimax") to real model IDs.
	// Unknown model IDs pass through to the API unchanged.
	if opts.Model != "" {
		if resolved, err := p.Resolve(opts.Model); err == nil {
			opts.Model = resolved.ID
		}
	}

	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil || apiKey == "" {
		return nil, llm.NewErrMissingAPIKey(providerName)
	}

	apiReq, err := anthropic.BuildRequest(anthropic.RequestOptions{
		LLMRequest: opts,
	})
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	// MiniMax's Anthropic-compatible endpoint does not support the thinking
	// field at all: the official Mini-Agent reference client never sends it
	// and the model thinks by default regardless. Omit the field unconditionally
	// so the request matches what MiniMax actually expects.
	// Ref: https://github.com/MiniMax-AI/Mini-Agent
	apiReq = adjustThinkingForMiniMax(apiReq)

	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	// Build http.Request first so URL, method, and headers are available for
	// the RequestEvent. The request is fully constructed here but not yet sent.
	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	parseOpts := anthropic.ParseOpts{
		Model:         opts.Model,
		ProviderName:  providerName,
		CostFn:        FillCost,
		LLMRequest:    opts,
		RequestParams: llm.ProviderRequestFromHTTP(req, body),
	}

	// Create publisher and emit RequestEvent BEFORE the HTTP call.
	pub, ch := llm.NewEventPublisher()
	anthropic.PublishRequestParams(pub, parseOpts)

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(providerName, err)
	}

	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return nil, apiErr
		}
		pub.Error(apiErr)
		pub.Close()
		return ch, nil
	}

	anthropic.ParseStreamWith(ctx, resp.Body, pub, parseOpts)
	return ch, nil
}

// adjustThinkingForMiniMax removes the thinking field from the request.
// MiniMax's Anthropic-compatible endpoint does not accept the thinking
// parameter: the official Mini-Agent reference client never sends it and
// MiniMax models produce thinking blocks by default regardless of whether
// the field is present. Sending "disabled" or any other value causes
// unspecified behaviour, so the field is always omitted.
// Ref: https://github.com/MiniMax-AI/Mini-Agent
func adjustThinkingForMiniMax(req anthropic.Request) anthropic.Request {
	req.Thinking = nil
	return req
}

func (p *Provider) newAPIRequest(ctx context.Context, apiKey string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
	req.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return req, nil
}
