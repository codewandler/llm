package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderNameAndModels(t *testing.T) {
	t.Parallel()

	p := New()
	assert.Equal(t, providerName, p.Name())
	models := p.Models()
	require.NotEmpty(t, models)
	assert.Equal(t, providerName, models[0].Provider)
}

func TestNewAPIRequestHeaders(t *testing.T) {
	t.Parallel()

	p := New(llm.WithBaseURL("https://example.test"), llm.WithAPIKey("k"))
	req, err := p.newAPIRequest(context.Background(), "token-123", []byte(`{"ok":true}`))
	require.NoError(t, err)

	assert.Equal(t, "https://example.test/v1/messages", req.URL.String())
	assert.Equal(t, "token-123", req.Header.Get("x-api-key"))
	assert.Equal(t, anthropicVersion, req.Header.Get("Anthropic-Version"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

func TestBuildAnthropicRequest_SystemAndTools(t *testing.T) {
	t.Parallel()

	body, err := buildAnthropicRequest(llm.StreamOptions{
		Model: "claude-sonnet-4-5-20250929",
		Messages: llm.Messages{
			&llm.SystemMsg{Content: "system prompt"},
			&llm.UserMsg{Content: "hello"},
		},
		Tools: []llm.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  map[string]any{"type": "object"},
		}},
		ToolChoice: llm.ToolChoiceAuto{},
	})
	require.NoError(t, err)

	var req request
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "system prompt", req.System)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "get_weather", req.Tools[0].Name)
	require.NotNil(t, req.ToolChoice)
}
