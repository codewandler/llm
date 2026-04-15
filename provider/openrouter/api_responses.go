package openrouter

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/api/unified"
)

func (p *Provider) streamResponses(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	body, err := buildOpenRouterResponsesBodyUnified(opts)
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		p.opts.BaseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
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

	respClient := responses.NewClient(
		responses.WithBaseURL(p.opts.BaseURL),
		responses.WithHTTPClient(&http.Client{
			Transport: &singleResponseTransport{resp: resp},
		}),
	)
	// wireReq is minimal — the real request body was already sent; the client
	// just needs a non-nil *responses.Request to call Stream().
	wireReq := &responses.Request{Model: opts.Model, Stream: true}
	handle, err := respClient.Stream(ctx, wireReq)
	if err != nil {
		pub.Error(err)
		pub.Close()
		return
	}

	go func() {
		defer pub.Close()
		unified.StreamResponses(ctx, handle, pub, unified.StreamContext{
			Provider:         providerName,
			Model:            opts.Model,
			UpstreamProvider: upstreamProviderFromModel(opts.Model),
		})
	}()
}
