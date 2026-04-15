package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/unified"
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
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

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

	uReq, err := unified.RequestFromLLM(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}
	wireReq, err := unified.RequestToMessages(uReq)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	body, err := json.MarshalIndent(wireReq, "", "  ")
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	// Build http.Request first so URL, method, and headers are available for
	// the RequestEvent. The request is fully constructed here but not yet sent.
	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameAnthropic, err)
	}

	// Create publisher and emit RequestEvent BEFORE the HTTP call
	// so consumers see what was requested even if the call fails.
	pub, ch := llm.NewEventPublisher()
	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: llm.ApiTypeAnthropicMessages,
	})

	// Emit token estimates: heuristic (local BPE) + API (exact count).
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model:    opts.Model,
		Messages: opts.Messages,
		Tools:    opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, llm.ProviderNameAnthropic, opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	// API count (exact, free endpoint — adds one HTTP round-trip)
	if apiCount, err := p.CountTokensAPI(ctx, wireReq); err == nil {
		apiEst := &tokencount.TokenCount{InputTokens: apiCount}
		for _, rec := range tokencount.EstimateRecords(apiEst, llm.ProviderNameAnthropic, opts.Model, "api", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(llm.ProviderNameAnthropic, err)
	}

	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(llm.ProviderNameAnthropic, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return nil, apiErr
		}
		pub.Error(apiErr)
		pub.Close()
		return ch, nil
	}

	// Parse response headers for rate limits.
	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}
	rateLimits := llm.ParseRateLimits(headers)

	// Stream the response body through the unified pipeline.
	// We use messages.NewClient to get a properly parsed handle,
	// feeding the already-received response body through a fake transport.
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

	sctx := unified.StreamContext{
		Provider:   providerName,
		Model:      opts.Model,
		CostCalc:   usage.Default(),
		RateLimits: rateLimits,
	}

	go func() {
		defer pub.Close()
		unified.StreamMessages(ctx, handle, pub, sctx)
	}()

	return ch, nil
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
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("Anthropic-Beta", BetaInterleavedThinking)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}
