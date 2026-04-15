package providercore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

// Client orchestrates request building, HTTP execution, and stream parsing for
// OpenAI-compatible APIs based on the provided Config.
type Client struct {
	cfg    Config
	opts   *llm.Options
	client *http.Client
}

// New constructs a Client with the given Config and llm.Options.
func New(cfg Config, llmOpts ...llm.Option) *Client {
	applyDefaults(&cfg)

	opts := llm.Apply(llmOpts...)
	return &Client{cfg: cfg, opts: opts, client: resolveHTTPClient(opts)}
}

// WithOptions clones the client with additional llm.Options applied.
func (c *Client) WithOptions(optFns ...llm.Option) *Client {
	if len(optFns) == 0 {
		return c
	}
	base := llm.Options{}
	if c.opts != nil {
		base = *c.opts
	}
	for _, opt := range optFns {
		opt(&base)
	}
	opts := new(llm.Options)
	*opts = base
	return &Client{cfg: c.cfg, opts: opts, client: resolveHTTPClient(opts)}
}

// Stream builds the request from src and streams the response using the
// configured API hint. On error, no stream is returned.
func (c *Client) Stream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	req, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	}

	requestedModel := req.Model
	if req.Model == "" || req.Model == llm.ModelDefault {
		if c.cfg.DefaultModel != "" {
			req.Model = c.cfg.DefaultModel
		}
	}

	if err := req.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	}

	if c.cfg.PreprocessRequest != nil {
		var orig string
		req, orig, err = c.cfg.PreprocessRequest(req)
		if err != nil {
			var provErr *llm.ProviderError
			if errors.As(err, &provErr) {
				return nil, err
			}
			return nil, llm.NewErrBuildRequest(c.cfg.ProviderName, err)
		}
		if orig != "" {
			requestedModel = orig
		} else {
			requestedModel = req.Model
		}
	}

	apiHint := c.cfg.APIHint
	if c.cfg.ResolveAPIHint != nil {
		if hint := c.cfg.ResolveAPIHint(req); hint != llm.ApiTypeAuto {
			apiHint = hint
		}
	}

	wireBody, bodyBytes, err := c.buildWireRequest(req, apiHint)
	if err != nil {
		return nil, llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	}

	httpReq, baseURL, err := c.buildHTTPRequest(ctx, req, bodyBytes, apiHint)
	if err != nil {
		return nil, err
	}

	pub, ch := llm.NewEventPublisher()

	if requestedModel != "" && requestedModel != req.Model {
		pub.ModelResolved(c.cfg.ProviderName, requestedModel, req.Model)
	}

	c.emitTokenEstimates(ctx, pub, req)

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: req,
		ProviderRequest: llm.ProviderRequestFromHTTP(httpReq, bodyBytes),
		ResolvedApiType: apiHint,
	})

	resp, err := c.client.Do(httpReq)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(c.cfg.ProviderName, err)
	}

	if resp.StatusCode/100 != 2 {
		apiErr := c.buildAPIError(resp)
		pub.Close()
		return ch, apiErr
	}

	rateLimits := parseRateLimits(resp, c.cfg.RateLimitParser)
	usageExtras := map[string]any(nil)
	if c.cfg.UsageExtras != nil {
		usageExtras = c.cfg.UsageExtras(resp)
	}

	streamHandle, err := c.buildStreamHandle(ctx, resp, wireBody, apiHint, baseURL)
	if err != nil {
		pub.Close()
		return nil, llm.AsProviderError(c.cfg.ProviderName, err)
	}

	upstream := c.resolveUpstream(req)
	costProvider, costModel := c.resolveCostTargets(req, upstream)

	streamCtx := unified.StreamContext{
		Provider:         c.cfg.ProviderName,
		Model:            req.Model,
		UpstreamProvider: upstream,
		CostCalc:         c.cfg.CostCalculator,
		CostProvider:     costProvider,
		CostModel:        costModel,
		RateLimits:       rateLimits,
		UsageExtras:      usageExtras,
	}

	go func() {
		defer pub.Close()
		switch apiHint {
		case llm.ApiTypeOpenAIChatCompletion:
			unified.ForwardCompletions(ctx, streamHandle, pub, streamCtx)
		case llm.ApiTypeAnthropicMessages:
			unified.ForwardMessages(ctx, streamHandle, pub, streamCtx)
		case llm.ApiTypeOpenAIResponses:
			unified.ForwardResponses(ctx, streamHandle, pub, streamCtx)
		default:
			pub.Error(fmt.Errorf("%s: unsupported API hint %s", c.cfg.ProviderName, apiHint))
		}
	}()

	return ch, nil
}

// buildWireRequest converts the unified request into the API-specific payload.
func (c *Client) buildWireRequest(req llm.Request, api llm.ApiType) (any, []byte, error) {
	uReq, err := unified.RequestFromLLM(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request from llm: %w", err)
	}

	marshalWire := func(wire any, marshalErrPrefix string) (any, []byte, error) {
		if c.cfg.TransformWireRequest != nil {
			wire, err = c.cfg.TransformWireRequest(api, wire)
			if err != nil {
				return nil, nil, err
			}
		}
		body, err := json.Marshal(wire)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", marshalErrPrefix, err)
		}
		return wire, body, nil
	}

	switch api {
	case llm.ApiTypeOpenAIChatCompletion:
		wire, err := unified.BuildCompletionsRequest(uReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request to completions: %w", err)
		}
		return marshalWire(wire, "marshal completions request")
	case llm.ApiTypeAnthropicMessages:
		wire, err := unified.BuildMessagesRequest(uReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request to messages: %w", err)
		}
		return marshalWire(wire, "marshal messages request")
	case llm.ApiTypeOpenAIResponses:
		wire, err := unified.BuildResponsesRequest(uReq)
		if err != nil {
			return nil, nil, fmt.Errorf("request to responses: %w", err)
		}
		return marshalWire(wire, "marshal responses request")
	default:
		return nil, nil, fmt.Errorf("unsupported api hint %s", api)
	}
}

func (c *Client) buildHTTPRequest(ctx context.Context, req llm.Request, body []byte, api llm.ApiType) (*http.Request, string, error) {
	baseURL := strings.TrimRight(resolveBaseURL(c.cfg, c.opts), "/")
	if baseURL == "" {
		return nil, "", llm.NewErrBuildRequest(c.cfg.ProviderName, fmt.Errorf("base URL is empty"))
	}

	path := c.cfg.BasePath
	if path == "" {
		path = defaultPathForAPI(api)
	}
	fullURL := baseURL + path

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return nil, "", llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	copyHeaders(httpReq.Header, c.cfg.DefaultHeaders)

	if c.cfg.HeaderFunc != nil {
		headers, err := c.cfg.HeaderFunc(ctx, &req)
		if err != nil {
			var provErr *llm.ProviderError
			if errors.As(err, &provErr) {
				return nil, "", err
			}
			return nil, "", llm.NewErrBuildRequest(c.cfg.ProviderName, err)
		}
		copyHeaders(httpReq.Header, headers)
	} else if key, err := c.opts.ResolveAPIKey(ctx); err != nil {
		return nil, "", llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	} else if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	if c.cfg.MutateRequest != nil {
		c.cfg.MutateRequest(httpReq)
	}

	return httpReq, baseURL, nil
}

func (c *Client) buildStreamHandle(ctx context.Context, resp *http.Response, wire any, api llm.ApiType, baseURL string) (*apicore.StreamHandle, error) {
	switch api {
	case llm.ApiTypeOpenAIChatCompletion:
		comp, ok := wire.(*completions.Request)
		if !ok {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("invalid completions payload type %T", wire)
		}
		client := completions.NewClient(
			completions.WithBaseURL(baseURL),
			completions.WithHTTPClient(&http.Client{Transport: &singleResponseTransport{resp: resp}}),
		)
		return client.Stream(ctx, comp)
	case llm.ApiTypeAnthropicMessages:
		msgReq, ok := wire.(*messages.Request)
		if !ok {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("invalid messages payload type %T", wire)
		}
		client := messages.NewClient(
			messages.WithBaseURL(baseURL),
			messages.WithHTTPClient(&http.Client{Transport: &singleResponseTransport{resp: resp}}),
		)
		return client.Stream(ctx, msgReq)
	case llm.ApiTypeOpenAIResponses:
		respReq, ok := wire.(*responses.Request)
		if !ok {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("invalid responses payload type %T", wire)
		}
		client := responses.NewClient(
			responses.WithBaseURL(baseURL),
			responses.WithHTTPClient(&http.Client{Transport: &singleResponseTransport{resp: resp}}),
		)
		return client.Stream(ctx, respReq)
	default:
		return nil, fmt.Errorf("unsupported api hint %s", api)
	}
}

func (c *Client) resolveUpstream(req llm.Request) string {
	if c.cfg.ResolveUpstreamProvider != nil {
		if upstream := c.cfg.ResolveUpstreamProvider(req); upstream != "" {
			return upstream
		}
	}
	return c.cfg.ProviderName
}

func (c *Client) resolveCostTargets(req llm.Request, upstream string) (string, string) {
	if c.cfg.ResolveCostTargets != nil {
		provider, model := c.cfg.ResolveCostTargets(req)
		if provider != "" || model != "" {
			return provider, model
		}
	}

	provider := upstream
	if provider == "" {
		provider = c.cfg.ProviderName
	}
	return provider, req.Model
}

func (c *Client) emitTokenEstimates(ctx context.Context, pub llm.Publisher, req llm.Request) {
	if c.cfg.TokenCounter == nil {
		return
	}

	count, err := c.cfg.TokenCounter.CountTokens(ctx, tokencount.TokenCountRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Tools:    req.Tools,
	})
	if err != nil || count == nil {
		return
	}

	records := tokencount.EstimateRecords(count, c.cfg.ProviderName, req.Model, "heuristic", c.cfg.CostCalculator)
	for _, rec := range records {
		pub.TokenEstimate(rec)
	}
}

func (c *Client) buildAPIError(resp *http.Response) error {
	// Centralize HTTP error parsing so future retry/response policies can inspect
	// the response metadata in one place without changing the request flow.
	defer resp.Body.Close()
	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if c.cfg.ErrorParser != nil {
		if apiErr := c.cfg.ErrorParser(resp.StatusCode, errBody); apiErr != nil {
			return apiErr
		}
	}
	return llm.NewErrAPIError(c.cfg.ProviderName, resp.StatusCode, string(errBody))
}

func applyDefaults(cfg *Config) {
	if cfg.ProviderName == "" {
		panic("providercore: ProviderName must be set")
	}
	if cfg.APIHint == llm.ApiTypeAuto || cfg.APIHint == "" {
		panic("providercore: APIHint must be a concrete API type")
	}
	if !cfg.costCalculatorSet {
		cfg.CostCalculator = usage.Default()
	}
	if cfg.DefaultHeaders == nil {
		cfg.DefaultHeaders = make(http.Header)
	}
}

func resolveHTTPClient(opts *llm.Options) *http.Client {
	if opts != nil && opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return llm.DefaultHttpClient()
}

func resolveBaseURL(cfg Config, opts *llm.Options) string {
	if opts != nil && opts.BaseURL != "" {
		return opts.BaseURL
	}
	return cfg.BaseURL
}

func defaultPathForAPI(api llm.ApiType) string {
	switch api {
	case llm.ApiTypeOpenAIChatCompletion:
		return "/v1/chat/completions"
	case llm.ApiTypeAnthropicMessages:
		return "/v1/messages"
	case llm.ApiTypeOpenAIResponses:
		return "/v1/responses"
	default:
		return ""
	}
}

func copyHeaders(dst http.Header, src http.Header) {
	if len(src) == 0 {
		return
	}
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func parseRateLimits(resp *http.Response, parser func(*http.Response) *llm.RateLimits) *llm.RateLimits {
	if parser == nil || resp == nil {
		return nil
	}
	return parser(resp)
}

type singleResponseTransport struct {
	resp *http.Response
}

func (t *singleResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	resp := t.resp
	t.resp = nil
	return resp, nil
}
