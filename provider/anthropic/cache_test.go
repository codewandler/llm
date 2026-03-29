package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

func TestBuildCacheControl(t *testing.T) {
	t.Run("nil hint returns nil", func(t *testing.T) {
		assert.Nil(t, buildCacheControl(nil))
	})

	t.Run("disabled hint returns nil", func(t *testing.T) {
		assert.Nil(t, buildCacheControl(&llm.CacheHint{Enabled: false}))
	})

	t.Run("enabled hint returns ephemeral with no ttl", func(t *testing.T) {
		cc := buildCacheControl(&llm.CacheHint{Enabled: true})
		require.NotNil(t, cc)
		assert.Equal(t, "ephemeral", cc.Type)
		assert.Equal(t, "", cc.TTL)
	})

	t.Run("enabled hint with 1h ttl", func(t *testing.T) {
		cc := buildCacheControl(&llm.CacheHint{Enabled: true, TTL: "1h"})
		require.NotNil(t, cc)
		assert.Equal(t, "ephemeral", cc.Type)
		assert.Equal(t, "1h", cc.TTL)
	})

	t.Run("enabled hint with 5m ttl omits ttl field", func(t *testing.T) {
		cc := buildCacheControl(&llm.CacheHint{Enabled: true, TTL: "5m"})
		require.NotNil(t, cc)
		assert.Equal(t, "", cc.TTL) // only "1h" is set explicitly; others use default
	})
}

func TestHasPerMessageCacheHints(t *testing.T) {
	t.Run("no messages returns false", func(t *testing.T) {
		assert.False(t, hasPerMessageCacheHints(nil))
	})

	t.Run("messages without hints returns false", func(t *testing.T) {
		msgs := msg.BuildTranscript(
			msg.System("system"),
			msg.User("user"),
		)
		assert.False(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("System with hint returns true", func(t *testing.T) {
		msgs := msg.BuildTranscript(
			msg.System("system").Cache().Build(),
		)
		assert.True(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("UserMsg with hint returns true", func(t *testing.T) {
		msgs := msg.BuildTranscript(
			msg.User("user").Cache().Build(),
		)
		assert.True(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("IsAssistantMsg with hint returns true", func(t *testing.T) {
		msgs := msg.BuildTranscript(
			msg.Assistant(msg.Text("reply")).Cache().Build(),
		)
		assert.True(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("disabled hint does not count", func(t *testing.T) {
		msgs := msg.BuildTranscript(
			msg.User("hi").Build(),
		)
		assert.False(t, hasPerMessageCacheHints(msgs))
	})
}

func TestBuildRequest_CacheHint_TopLevel(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello"),
			),
			CacheHint: &llm.CacheHint{Enabled: true},
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	// Top-level cache_control must be present
	cc, ok := req["cache_control"].(map[string]any)
	require.True(t, ok, "expected cache_control at top level")
	assert.Equal(t, "ephemeral", cc["type"])
}

func TestBuildRequest_CacheHint_NoTopLevelWhenPerMessageHintsExist(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello").Cache().Build(),
			),
			CacheHint: &llm.CacheHint{Enabled: true}, // should be ignored
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	// Top-level cache_control must NOT be present when per-message hints exist
	_, hasTopLevel := req["cache_control"]
	assert.False(t, hasTopLevel, "top-level cache_control should not be set when per-message hints exist")
}

func TestBuildRequest_CacheHint_PerMessageUser(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello").Cache().Build(),
			),
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	messages := req["messages"].([]any)
	require.Len(t, messages, 1)

	content := messages[0].(map[string]any)["content"].([]any)
	require.Len(t, content, 1)

	block := content[0].(map[string]any)
	cc, ok := block["cache_control"].(map[string]any)
	require.True(t, ok, "expected cache_control on user content block")
	assert.Equal(t, "ephemeral", cc["type"])
}

func TestBuildRequest_CacheHint_SystemBlock(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.System("Big prompt").Cache().Build(),
				msg.User("Hello"),
			),
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	system := req["system"].([]any)
	require.Len(t, system, 1)

	block := system[0].(map[string]any)
	cc, ok := block["cache_control"].(map[string]any)
	require.True(t, ok, "expected cache_control on system block")
	assert.Equal(t, "ephemeral", cc["type"])
}

func TestBuildRequest_CacheHint_ExtendedTTL(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello"),
			),
			CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	cc := req["cache_control"].(map[string]any)
	assert.Equal(t, "ephemeral", cc["type"])
	assert.Equal(t, "1h", cc["ttl"])
}

func TestBuildRequest_NoCacheHint_NoTopLevelField(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello"),
			),
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	_, hasCC := req["cache_control"]
	assert.False(t, hasCC, "no cache_control should be present without CacheHint")
}

func TestCollectSystemBlocks_CacheHint(t *testing.T) {
	blocks, _ := convertMessages(msg.BuildTranscript(
		msg.System("First").Cache().Build(),
		msg.System("Second"),
	))

	require.Len(t, blocks, 2)

	// First block should have cache_control
	require.NotNil(t, blocks[0].CacheControl)
	assert.Equal(t, "ephemeral", blocks[0].CacheControl.Type)

	// Second block should not
	assert.Nil(t, blocks[1].CacheControl)
}

func TestBuildRequest_AssistantWithBlocksAndCacheHint_TextBlock(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello"),
				msg.Assistant(
					msg.Text("world"),
				).Cache().Build(),
			),
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	messages := req["messages"].([]any)
	require.Len(t, messages, 2)

	// Second message is the assistant with cache hint
	assistant := messages[1].(map[string]any)
	content := assistant["content"].([]any)
	require.Len(t, content, 1)

	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "world", block["text"])

	// Cache control should be on the text block
	cc, ok := block["cache_control"].(map[string]any)
	require.True(t, ok, "expected cache_control on text block")
	require.Equal(t, "ephemeral", cc["type"])
}

func TestBuildRequest_AssistantWithBlocksAndCacheHint_ThinkingBlock(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello"),
				msg.Assistant(
					msg.Thinking("Thinking text", "sig123"),
					msg.Text("answer"),
				).Cache().Build(),
			),
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	messages := req["messages"].([]any)
	require.Len(t, messages, 2)

	assistant := messages[1].(map[string]any)
	content := assistant["content"].([]any)
	// Thought block is filtered out; only text block remains
	require.Len(t, content, 1)

	// Only text block with cache_control
	textBlock := content[0].(map[string]any)
	assert.Equal(t, "text", textBlock["type"])
	assert.Equal(t, "answer", textBlock["text"])

	cc := textBlock["cache_control"].(map[string]any)
	assert.Equal(t, "ephemeral", cc["type"])
}

func TestBuildRequest_AssistantWithBlocksAndCacheHint_ThinkingBlock_LastBlock(t *testing.T) {
	opts := RequestOptions{
		LLMRequest: llm.Request{
			Model:     "claude-sonnet-4-6",
			MaxTokens: 100,
			Messages: msg.BuildTranscript(
				msg.User("Hello"),
				msg.Assistant(
					msg.Thinking("Thinking text", "sig123"),
					msg.Text("answer"),
				).Cache().Build(),
			),
		},
	}

	data, err := BuildRequest(opts)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(data, &req))

	messages := req["messages"].([]any)
	assistant := messages[1].(map[string]any)
	content := assistant["content"].([]any)

	// Only text block; ThinkingConfig is filtered out
	require.Len(t, content, 1)
	textBlock := content[0].(map[string]any)
	assert.Equal(t, "text", textBlock["type"])
	assert.Equal(t, "answer", textBlock["text"])

	// Cache control on text block
	cc := textBlock["cache_control"].(map[string]any)
	assert.Equal(t, "ephemeral", cc["type"])
}
