package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamResponsesUnified_RequestBodyParity(t *testing.T) {
	opts := llm.Request{
		Model:      "openai/gpt-5.4",
		MaxTokens:  128,
		Effort:     llm.EffortHigh,
		ToolChoice: llm.ToolChoiceRequired{},
		Tools:      []tool.Definition{{Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}}},
		Messages:   llm.Messages{llm.System("sys"), llm.User("hello")},
	}

	legacyBody, err := buildOpenRouterResponsesBodyLegacy(opts)
	require.NoError(t, err)

	var gotBody []byte
	httpClient := &http.Client{Transport: apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "/v1/responses", r.URL.Path)
		b, readErr := io.ReadAll(r.Body)
		require.NoError(t, readErr)
		gotBody = b
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"openai/gpt-5.4\",\"status\":\"completed\"}}\n\n")),
		}, nil
	})}

	p := New(
		llm.WithHTTPClient(httpClient),
		llm.WithAPIKey("sk-test"),
		llm.WithBaseURL("https://openrouter.test"),
	)

	stream, err := p.CreateStream(context.Background(), opts)
	require.NoError(t, err)
	for range stream {
	}

	require.NotEmpty(t, gotBody)

	var got map[string]any
	var want map[string]any
	require.NoError(t, json.Unmarshal(gotBody, &got))
	require.NoError(t, json.Unmarshal(legacyBody, &want))
	assert.Equal(t, want, got)
}
