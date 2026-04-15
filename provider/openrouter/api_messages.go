package openrouter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/provider/anthropic"
)

func (p *Provider) streamMessages(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	strippedModel := strings.TrimPrefix(opts.Model, "anthropic/")

	body, err := buildOpenRouterMessagesBodyUnified(opts)
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey) // OpenRouter uses Bearer, NOT x-api-key
	req.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
	req.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts, // preserves "anthropic/claude-opus-4-5" for correlation
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: resolvedApiType,
	})

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Error(llm.NewErrRequestFailed(providerName, err))
		pub.Close()
		return
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return
		}
		pub.Error(apiErr)
		pub.Close()
		return
	}

	// Feed the already-received response body into a messages.Client to get
	// a properly parsed handle, then run through the unified stream pipeline.
	// UpstreamProvider = "anthropic" so StreamStartedEvent.Provider is correct.
	msgClient := messages.NewClient(
		messages.WithBaseURL(p.opts.BaseURL),
		messages.WithHTTPClient(&http.Client{
			Transport: &singleResponseTransport{resp: resp},
		}),
	)
	wireReq := &messages.Request{Model: strippedModel, Stream: true}
	handle, err := msgClient.Stream(ctx, wireReq)
	if err != nil {
		pub.Error(err)
		pub.Close()
		return
	}

	go func() {
		defer pub.Close()
		unified.StreamMessages(ctx, handle, pub, unified.StreamContext{
			Provider:         providerName,
			Model:            strippedModel,
			UpstreamProvider: "anthropic",
		})
	}()
}

// singleResponseTransport is an http.RoundTripper that returns a pre-built
// *http.Response exactly once. Used to feed an already-received response body
// into a messages.Client without making a second HTTP call.
type singleResponseTransport struct {
	resp *http.Response
}

func (t *singleResponseTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return t.resp, nil
}
