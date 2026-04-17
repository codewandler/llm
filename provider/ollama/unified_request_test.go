package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateStream_UsesResponsesRequestBody(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotBody map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		defer r.Body.Close()
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(bodyBytes, &gotBody))

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"event: response.created\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"llama3.2\"}}\n\n"+
				"event: response.output_text.delta\ndata: {\"output_index\":0,\"delta\":\"pong\"}\n\n"+
				"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"llama3.2\",\"status\":\"completed\",\"usage\":{\"input_tokens\":11,\"output_tokens\":4}}}\n\n",
		)
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL))
	stream, err := p.CreateStream(context.Background(), llm.Request{
		Model:        "llama3.2",
		MaxTokens:    128,
		Temperature:  0.2,
		TopP:         0.9,
		TopK:         20,
		OutputFormat: llm.OutputFormatJSON,
		Tools: []tool.Definition{{
			Name:        "search",
			Description: "Search docs",
			Parameters:  map[string]any{"type": "object"},
		}},
		ToolChoice: llm.ToolChoiceRequired{},
		Messages: llm.Messages{
			llm.System("system"),
			llm.User("hello"),
		},
	})
	require.NoError(t, err)
	for range stream {
	}

	assert.Equal(t, "/v1/responses", gotPath)
	require.NotNil(t, gotBody)
	assert.Equal(t, "llama3.2", gotBody["model"])
	assert.Equal(t, float64(128), gotBody["max_output_tokens"])
	assert.Equal(t, 0.2, gotBody["temperature"])
	assert.Equal(t, 0.9, gotBody["top_p"])
	assert.Equal(t, float64(20), gotBody["top_k"])
	assert.Equal(t, true, gotBody["stream"])
	assert.Equal(t, "system", gotBody["instructions"])
	require.Len(t, gotBody["input"].([]any), 1)
	assert.Equal(t, "user", gotBody["input"].([]any)[0].(map[string]any)["role"])
	assert.Equal(t, "hello", gotBody["input"].([]any)[0].(map[string]any)["content"])
	require.NotNil(t, gotBody["response_format"])
	assert.Equal(t, "json_object", gotBody["response_format"].(map[string]any)["type"])
	require.Len(t, gotBody["tools"].([]any), 1)
	assert.Equal(t, "required", gotBody["tool_choice"])
	_, hasMessages := gotBody["messages"]
	_, hasNumPredict := gotBody["num_predict"]
	_, hasFormat := gotBody["format"]
	assert.False(t, hasMessages)
	assert.False(t, hasNumPredict)
	assert.False(t, hasFormat)
}

func TestCreateStream_ErrorsOnMultipleSystemMessages(t *testing.T) {
	t.Parallel()

	p := New()
	_, err := p.CreateStream(context.Background(), llm.Request{
		Model: "llama3.2",
		Messages: llm.Messages{
			llm.System("sys1"),
			llm.System("sys2"),
			llm.User("hello"),
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple system messages")
}
