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
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
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

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

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

	uReq, err := unified.RequestFromLLM(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}
	wireReq, err := unified.RequestToMessages(uReq)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	// MiniMax's Anthropic-compatible endpoint does not support the thinking
	// field at all: the official Mini-Agent reference client never sends it
	// and the model thinks by default regardless. Omit the field unconditionally
	// so the request matches what MiniMax actually expects.
	// Ref: https://github.com/MiniMax-AI/Mini-Agent
	wireReq.Thinking = nil

	body, err := json.Marshal(wireReq)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	// Build http.Request first so URL, method, and headers are available for
	// the RequestEvent. The request is fully constructed here but not yet sent.
	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, llm.NewErrBuildRequest(providerName, err)
	}

	// Create publisher and emit RequestEvent BEFORE the HTTP call.
	pub, ch := llm.NewEventPublisher()
	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: llm.ApiTypeAnthropicMessages,
	})

	// Emit token estimates (primary + per-segment breakdown)
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, providerName, opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(providerName, err)
	}

	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
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

	msgClient := messages.NewClient(
		messages.WithBaseURL(p.opts.BaseURL),
		messages.WithHTTPClient(&http.Client{
			Transport: &singleResponseTransport{resp: resp},
		}),
	)
	handle, err := msgClient.Stream(ctx, wireReq)
	if err != nil {
		pub.Error(err)
		pub.Close()
		return ch, nil
	}

	go func() {
		defer pub.Close()
		unified.StreamMessages(ctx, handle, pub, unified.StreamContext{
			Provider: providerName,
			Model:    opts.Model,
			CostCalc: usage.Default(),
		})
	}()

	return ch, nil
}

// adjustThinkingForMiniMax removes the thinking field from the request.
// MiniMax's Anthropic-compatible endpoint does not accept the thinking
// parameter: the official Mini-Agent reference client never sends it and
// MiniMax models produce thinking blocks by default regardless of whether
// the field is present. Sending "disabled" or any other value causes
// unspecified behaviour, so the field is always omitted.
// Ref: https://github.com/MiniMax-AI/Mini-Agent
//
// Deprecated: inline wireReq.Thinking = nil in CreateStream; kept for tests.
func adjustThinkingForMiniMax(req anthropic.Request) anthropic.Request {
	req.Thinking = nil
	return req
}

// singleResponseTransport is an http.RoundTripper that returns a pre-built
// *http.Response exactly once. Used to feed an already-received response
// body into a messages.Client without making a second HTTP call.
type singleResponseTransport struct {
	resp *http.Response
}

func (t *singleResponseTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return t.resp, nil
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
