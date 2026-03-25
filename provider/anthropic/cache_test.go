package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
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
		msgs := llm.Messages{
			llm.System("system"),
			llm.User("user"),
		}
		assert.False(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("System with hint returns true", func(t *testing.T) {
		msgs := llm.Messages{
			llm.System("system", &llm.CacheHint{Enabled: true}),
		}
		assert.True(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("UserMsg with hint returns true", func(t *testing.T) {
		msgs := llm.Messages{
			llm.User("hi", &llm.CacheHint{Enabled: true}),
		}
		assert.True(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("AssistantMessage with hint returns true", func(t *testing.T) {
		msgs := llm.Messages{
			llm.AssistantWithCacheHint("reply", &llm.CacheHint{Enabled: true}),
		}
		assert.True(t, hasPerMessageCacheHints(msgs))
	})

	t.Run("disabled hint does not count", func(t *testing.T) {
		msgs := llm.Messages{
			llm.User("hi", &llm.CacheHint{Enabled: false}),
		}
		assert.False(t, hasPerMessageCacheHints(msgs))
	})
}

func TestBuildRequest_CacheHint_TopLevel(t *testing.T) {
	opts := RequestOptions{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		StreamOptions: llm.Request{
			Messages: llm.Messages{
				llm.User("Hello"),
			},
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
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		StreamOptions: llm.Request{
			Messages: llm.Messages{
				llm.User("Hello", &llm.CacheHint{Enabled: true}),
			},
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
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		StreamOptions: llm.Request{
			Messages: llm.Messages{
				llm.User("Hello", &llm.CacheHint{Enabled: true}),
			},
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
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		StreamOptions: llm.Request{
			Messages: llm.Messages{
				llm.System("Big prompt", &llm.CacheHint{Enabled: true}),
				llm.User("Hello"),
			},
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
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		StreamOptions: llm.Request{
			Messages: llm.Messages{
				llm.User("Hello"),
			},
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
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		StreamOptions: llm.Request{
			Messages: llm.Messages{
				llm.User("Hello"),
			},
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
	msgs := llm.Messages{
		llm.System("First", &llm.CacheHint{Enabled: true}),
		llm.System("Second"),
	}

	blocks := CollectSystemBlocks(msgs)
	require.Len(t, blocks, 2)

	// First block should have cache_control
	require.NotNil(t, blocks[0].CacheControl)
	assert.Equal(t, "ephemeral", blocks[0].CacheControl.Type)

	// Second block should not
	assert.Nil(t, blocks[1].CacheControl)
}
