package providercore

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/codewandler/agentapis/adapt"
	agentclient "github.com/codewandler/agentapis/client"
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

// Client orchestrates request preprocessing, agentapis streaming, and llm event publication.
type Client struct {
	cfg    clientConfig
	opts   *llm.Options
	client *http.Client
}

func New(cfg clientConfig, llmOpts ...llm.Option) *Client {
	applyDefaults(&cfg)
	opts := llm.Apply(llmOpts...)
	return &Client{cfg: cfg, opts: opts, client: resolveHTTPClient(opts)}
}

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

func (c *Client) Stream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	originalReq, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	}
	requestedModel := originalReq.Model
	resolvedReq := originalReq
	if c.cfg.PreprocessRequest != nil {
		var orig string
		resolvedReq, orig, err = c.cfg.PreprocessRequest(originalReq)
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
			requestedModel = resolvedReq.Model
		}
	}
	if err := resolvedReq.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(c.cfg.ProviderName, err)
	}
	apiHint := c.cfg.APIHint
	if c.cfg.ResolveAPIHint != nil {
		if hint := c.cfg.ResolveAPIHint(resolvedReq); hint != llm.ApiTypeAuto {
			apiHint = hint
		}
	}
	pub, ch := llm.NewEventPublisher()
	c.emitTokenEstimates(ctx, pub, resolvedReq, apiHint)
	typed := c.buildAgentClient(originalReq, resolvedReq, apiHint, requestedModel)
	stream, streamErr := typed.Stream(ctx, originalReq)
	if streamErr != nil {
		if stream == nil {
			pub.Close()
			return nil, c.finalizeImmediateError(resolvedReq, streamErr)
		}
		status, hasStatus := agentclient.StatusCodeOf(streamErr)
		action := HTTPErrorActionReturn
		if hasStatus && c.cfg.ResolveHTTPErrorAction != nil {
			action = c.cfg.ResolveHTTPErrorAction(resolvedReq, status, mapAgentStreamError(c.cfg.ProviderName, c.cfg.ErrorParser != nil, streamErr))
		}
		go func() {
			defer pub.Close()
			forwardTypedStream(c.cfg.ProviderName, pub, stream)
			if action == HTTPErrorActionStream {
				pub.Error(llm.AsProviderError(c.cfg.ProviderName, mapAgentStreamError(c.cfg.ProviderName, c.cfg.ErrorParser != nil, streamErr)))
			}
		}()
		if action == HTTPErrorActionStream {
			return ch, nil
		}
		return ch, mapAgentStreamError(c.cfg.ProviderName, c.cfg.ErrorParser != nil, streamErr)
	}
	go func() {
		defer pub.Close()
		forwardTypedStream(c.cfg.ProviderName, pub, stream)
	}()
	return ch, nil
}

func forwardTypedStream(provider string, pub llm.Publisher, stream <-chan agentclient.Result[llm.Event]) {
	for item := range stream {
		if item.Err != nil {
			pub.Error(llm.AsProviderError(provider, mapAgentStreamError(provider, false, item.Err)))
			return
		}
		if item.Event == nil {
			continue
		}
		pub.Publish(item.Event)
	}
}

func (c *Client) finalizeImmediateError(_ llm.Request, err error) error {
	return mapAgentStreamError(c.cfg.ProviderName, c.cfg.ErrorParser != nil, err)
}

func mapAgentStreamError(provider string, customParser bool, err error) error {
	if statusErr := agentclient.StatusErrorOf(err); statusErr != nil {
		if customParser && statusErr.Err != nil {
			return statusErr.Err
		}
		return llm.NewErrAPIError(provider, statusErr.StatusCode, string(statusErr.Body))
	}
	var provErr *llm.ProviderError
	if errors.As(err, &provErr) {
		return provErr
	}
	return llm.NewErrRequestFailed(provider, err)
}

func (c *Client) emitTokenEstimates(ctx context.Context, pub llm.Publisher, req llm.Request, apiHint llm.ApiType) {
	est := tokencount.Estimate(ctx, c.cfg.ProviderName, req)
	if est == nil {
		return
	}
	now := time.Now()
	rec := usage.Record{
		IsEstimate: true,
		Source:     "heuristic",
		Encoder:    est.Encoder,
		RecordedAt: now,
		Dims:       usage.Dims{Provider: c.cfg.ProviderName, Model: req.Model},
		Tokens:     est.Tokens,
		Cost:       est.Cost,
	}
	pub.TokenEstimate(rec)
	if c.cfg.MessagesAPITokenCounter != nil && apiHint == llm.ApiTypeAnthropicMessages {
		uReq, err := requestToAgentUnified(req)
		if err != nil {
			return
		}
		wire, err := adapt.BuildMessagesRequest(uReq)
		if err != nil {
			return
		}
		if c.cfg.MessagesRequestTransform != nil {
			if err := c.cfg.MessagesRequestTransform(wire); err != nil {
				return
			}
		}
		count, err := c.cfg.MessagesAPITokenCounter(ctx, req, wire)
		if err != nil || count == nil {
			return
		}
		for _, rec := range tokencount.EstimateRecords(count, c.cfg.ProviderName, req.Model, "api", usage.Default()) {
			pub.TokenEstimate(rec)
		}
		return
	}
	if c.cfg.APITokenCounter == nil {
		return
	}
	count, err := c.cfg.APITokenCounter(ctx, req, nil)
	if err != nil || count == nil {
		return
	}
	for _, rec := range tokencount.EstimateRecords(count, c.cfg.ProviderName, req.Model, "api", usage.Default()) {
		pub.TokenEstimate(rec)
	}
}

func applyDefaults(cfg *clientConfig) {
	if err := cfg.Validate(); err != nil {
		panic(err.Error())
	}
	cfg.ApplyDefaults()
}

func resolveHTTPClient(opts *llm.Options) *http.Client {
	if opts != nil && opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return llm.DefaultHttpClient()
}

func resolveBaseURL(cfg clientConfig, opts *llm.Options) string {
	if opts != nil && opts.BaseURL != "" {
		return opts.BaseURL
	}
	return cfg.BaseURL
}
