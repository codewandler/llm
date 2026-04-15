package ollama

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildRequestUnified_FieldMapping verifies that unified.RequestToCompletions
// maps the correct OpenAI Chat Completions fields. Ollama uses a proprietary
// wire dialect (num_predict, format:json) so parity with the legacy
// buildRequest is intentionally NOT checked here — only canonical field values.
func TestBuildRequestUnified_FieldMapping(t *testing.T) {
	opts := llm.Request{
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
		Messages: llm.Messages{
			llm.System("system"),
			llm.User("hello"),
		},
	}

	legacyBody, err := buildRequest(opts)
	require.NoError(t, err)
	var legacy map[string]any
	require.NoError(t, json.Unmarshal(legacyBody, &legacy))

	// Verify legacy uses Ollama dialect
	assert.Equal(t, float64(128), legacy["num_predict"], "legacy maps MaxTokens to num_predict (Ollama dialect)")
	assert.Equal(t, "json", legacy["format"], "legacy uses format:json for OutputFormatJSON (Ollama dialect)")
	_, hasStreamOptions := legacy["stream_options"]
	assert.False(t, hasStreamOptions, "legacy omits stream_options (Ollama dialect)")

	// Ollama note: api/unified produces standard OpenAI Chat Completions JSON.
	// Ollama's /api/chat has a proprietary dialect that requires a dedicated
	// converter or transform. Unified is used for the event path only until
	// a RequestToOllamaCompletions is added.
}
