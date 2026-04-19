package providercore

import (
	"context"
	"net/http"

	completionsapi "github.com/codewandler/agentapis/api/completions"
	messagesapi "github.com/codewandler/agentapis/api/messages"
	responsesapi "github.com/codewandler/agentapis/api/responses"
	agentclient "github.com/codewandler/agentapis/client"
	"github.com/codewandler/llm"
)

func (c *Client) buildAgentClient(originalReq, resolvedReq llm.Request, apiHint llm.ApiType, requestedModel string) *agentclient.TypedClient[llm.Request, llm.Event] {
	baseURL := resolveBaseURL(c.cfg, c.opts)
	path := c.cfg.BasePath

	messageOpts := []messagesapi.Option{
		messagesapi.WithBaseURL(baseURL),
		messagesapi.WithHTTPClient(c.client),
		messagesapi.WithErrorParser(c.cfg.ErrorParser),
	}
	if path != "" {
		messageOpts = append(messageOpts, messagesapi.WithPath(path))
	}
	for key, values := range c.cfg.DefaultHeaders {
		for _, value := range values {
			messageOpts = append(messageOpts, messagesapi.WithHeader(key, value))
		}
	}
	messageOpts = append(messageOpts,
		messagesapi.WithHeaderFunc(func(ctx context.Context, _ *messagesapi.Request) (http.Header, error) {
			return c.resolveHeaders(ctx, resolvedReq, apiHint)
		}),
		messagesapi.WithRequestTransform(func(ctx context.Context, wire *messagesapi.Request) error {
			if wire != nil && wire.Thinking != nil && wire.Thinking.Type == "adaptive" && wire.Temperature != 0 && wire.Temperature != 1 {
				wire.Temperature = 1
			}
			if c.cfg.MessagesRequestTransform != nil {
				return c.cfg.MessagesRequestTransform(wire)
			}
			return nil
		}),
		messagesapi.WithHTTPRequestMutator(func(ctx context.Context, httpReq *http.Request, _ *messagesapi.Request) error {
			if c.cfg.MutateRequest != nil {
				c.cfg.MutateRequest(httpReq)
			}
			return nil
		}),
	)

	completionsOpts := []completionsapi.Option{
		completionsapi.WithBaseURL(baseURL),
		completionsapi.WithHTTPClient(c.client),
		completionsapi.WithErrorParser(c.cfg.ErrorParser),
	}
	if path != "" {
		completionsOpts = append(completionsOpts, completionsapi.WithPath(path))
	}
	for key, values := range c.cfg.DefaultHeaders {
		for _, value := range values {
			completionsOpts = append(completionsOpts, completionsapi.WithHeader(key, value))
		}
	}
	completionsOpts = append(completionsOpts,
		completionsapi.WithHeaderFunc(func(ctx context.Context, _ *completionsapi.Request) (http.Header, error) {
			return c.resolveHeaders(ctx, resolvedReq, apiHint)
		}),
		completionsapi.WithRequestTransform(func(ctx context.Context, wire *completionsapi.Request) error {
			if c.cfg.CompletionsRequestTransform != nil {
				return c.cfg.CompletionsRequestTransform(wire)
			}
			return nil
		}),
		completionsapi.WithHTTPRequestMutator(func(ctx context.Context, httpReq *http.Request, _ *completionsapi.Request) error {
			if c.cfg.MutateRequest != nil {
				c.cfg.MutateRequest(httpReq)
			}
			return nil
		}),
	)

	responsesOpts := []responsesapi.Option{
		responsesapi.WithBaseURL(baseURL),
		responsesapi.WithHTTPClient(c.client),
		responsesapi.WithErrorParser(c.cfg.ErrorParser),
	}
	if path != "" {
		responsesOpts = append(responsesOpts, responsesapi.WithPath(path))
	}
	for key, values := range c.cfg.DefaultHeaders {
		for _, value := range values {
			responsesOpts = append(responsesOpts, responsesapi.WithHeader(key, value))
		}
	}
	responsesOpts = append(responsesOpts,
		responsesapi.WithHeaderFunc(func(ctx context.Context, _ *responsesapi.Request) (http.Header, error) {
			return c.resolveHeaders(ctx, resolvedReq, apiHint)
		}),
		responsesapi.WithRequestTransform(func(ctx context.Context, wire *responsesapi.Request) error {
			if c.cfg.ResponsesRequestTransform != nil {
				return c.cfg.ResponsesRequestTransform(wire)
			}
			return nil
		}),
		responsesapi.WithHTTPRequestMutator(func(ctx context.Context, httpReq *http.Request, _ *responsesapi.Request) error {
			if c.cfg.MutateRequest != nil {
				c.cfg.MutateRequest(httpReq)
			}
			return nil
		}),
	)

	upstream := agentclient.NewMuxClient(
		agentclient.WithMessagesClient(agentclient.NewMessagesClient(messagesapi.NewClient(messageOpts...))),
		agentclient.WithCompletionsClient(agentclient.NewCompletionsClient(completionsapi.NewClient(completionsOpts...))),
		agentclient.WithResponsesClient(agentclient.NewResponsesClient(responsesapi.NewClient(responsesOpts...))),
	)

	return agentclient.NewTypedClient[llm.Request, llm.Event](upstream, llmBridgeBuilder{
		cfg:            c.cfg,
		originalReq:    originalReq,
		resolvedReq:    resolvedReq,
		requestedModel: requestedModel,
		resolvedAPI:    apiHint,
	})
}

func (c *Client) resolveHeaders(ctx context.Context, req llm.Request, apiHint llm.ApiType) (http.Header, error) {
	if c.cfg.HeaderFunc != nil {
		return c.cfg.HeaderFunc(ctx, &req)
	}
	key, err := c.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, nil
	}
	switch apiHint {
	case llm.ApiTypeAnthropicMessages:
		return http.Header{messagesapi.HeaderAPIKey: {key}}, nil
	default:
		return http.Header{"Authorization": {"Bearer " + key}}, nil
	}
}
