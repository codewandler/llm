package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/codewandler/llm/api/messages"
)

// countTokensRequest is the request body for /v1/messages/count_tokens.
// It mirrors the Messages API Request but omits stream/max_tokens since
// the count endpoint doesn't need them.
type countTokensRequest struct {
	Model        string                    `json:"model"`
	Messages     []messages.Message        `json:"messages"`
	System       messages.SystemBlocks     `json:"system,omitempty"`
	Tools        []messages.ToolDefinition `json:"tools,omitempty"`
	ToolChoice   any                       `json:"tool_choice,omitempty"`
	Thinking     *messages.ThinkingConfig  `json:"thinking,omitempty"`
	CacheControl *messages.CacheControl    `json:"cache_control,omitempty"`
}

// countTokensResponse is the response from /v1/messages/count_tokens.
type countTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// CountTokensAPI calls the Anthropic /v1/messages/count_tokens endpoint
// to get an exact token count for the given request. This is free and does
// not consume message rate limits, but adds one HTTP round-trip.
//
// The returned count is exact (not a heuristic approximation). It includes
// tool definitions, system prompts, images, PDFs, and thinking blocks.
func (p *Provider) CountTokensAPI(ctx context.Context, apiReq *messages.Request) (int, error) {
	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return 0, fmt.Errorf("anthropic: count_tokens: %w", err)
	}
	if apiKey == "" {
		return 0, fmt.Errorf("anthropic: count_tokens: missing API key")
	}

	return DoCountTokensAPI(ctx, p.client, p.opts.BaseURL, apiKey, nil, apiReq)
}

// DoCountTokensAPI is the shared implementation for the Anthropic count_tokens
// endpoint, usable by both the direct Anthropic provider and the Claude OAuth
// provider. extraHeaders are appended to the request (e.g. OAuth Authorization).
func DoCountTokensAPI(
	ctx context.Context,
	client *http.Client,
	baseURL, apiKey string,
	extraHeaders map[string]string,
	apiReq *messages.Request,
) (int, error) {
	countReq := countTokensRequest{
		Model:        apiReq.Model,
		Messages:     apiReq.Messages,
		System:       apiReq.System,
		Tools:        apiReq.Tools,
		ToolChoice:   apiReq.ToolChoice,
		Thinking:     apiReq.Thinking,
		CacheControl: apiReq.CacheControl,
	}

	body, err := json.Marshal(countReq)
	if err != nil {
		return 0, fmt.Errorf("count_tokens: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		baseURL+"/v1/messages/count_tokens",
		bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("count_tokens: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", AnthropicVersion)
	req.Header.Set("Anthropic-Beta", BetaInterleavedThinking)
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("count_tokens: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("count_tokens: HTTP %d: %s",
			resp.StatusCode, string(errBody))
	}

	var result countTokensResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("count_tokens: decode: %w", err)
	}

	return result.InputTokens, nil
}
