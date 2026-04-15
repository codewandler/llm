package openrouter

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrRespBuildRequest_Basic(t *testing.T) {
	opts := llm.Request{
		Model:    "openai/gpt-5.4",
		Messages: llm.Messages{llm.User("hello")},
	}
	body, err := orRespBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	assert.Equal(t, "openai/gpt-5.4", req["model"],
		"model must not strip the openai/ prefix — OpenRouter uses full IDs")
	assert.Equal(t, true, req["stream"])
	assert.NotNil(t, req["input"], "input array must be present")
}

func TestOrRespBuildRequest_NoPromptCacheRetention(t *testing.T) {
	// orRespBuildRequest never sets prompt_cache_retention (OpenRouter doesn't
	// support it) so any request must produce a body without the field.
	opts := llm.Request{
		Model:    "openai/gpt-5.4",
		Messages: llm.Messages{llm.User("hi")},
	}
	body, err := orRespBuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Nil(t, req["prompt_cache_retention"],
		"OpenRouter does not support prompt_cache_retention — must not be set")
}
