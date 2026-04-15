package openrouter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
)

func (p *Provider) streamMessages(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	// Strip "anthropic/" prefix: OpenRouter's /v1/messages expects bare model IDs
	// (e.g. "claude-opus-4-5", not "anthropic/claude-opus-4-5").
	// We strip only for the wire body and ParseStreamWith; the original model ID
	// is preserved in RequestEvent.OriginalRequest for request correlation.
	strippedModel := strings.TrimPrefix(opts.Model, "anthropic/")

	reqOpts := opts
	reqOpts.Model = strippedModel

	body, err := anthropic.BuildRequestBytes(anthropic.RequestOptions{LLMRequest: reqOpts})
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

	// ParseStreamWith spawns a goroutine internally and takes ownership of both
	// resp.Body and pub (closes pub when parsing completes).
	// ProviderName = "openrouter" labels all error events and usage records.
	// UpstreamProvider = "anthropic" sets StreamStartedEvent.Provider correctly.
	anthropic.ParseStreamWith(ctx, resp.Body, pub, anthropic.ParseOpts{
		Model:            strippedModel,
		ProviderName:     providerName,
		UpstreamProvider: "anthropic",
	})
}
