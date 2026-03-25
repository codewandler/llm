package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestEnrichOpts_CacheHintTTL_OneHour(t *testing.T) {
	// CacheHint with TTL "1h" should make wantsExtendedCache return true,
	// and the built request should carry prompt_cache_retention: "24h".
	opts := llm.Request{
		Model: "gpt-4o-mini", // doesn't normally support extended cache
		Messages: llm.Messages{
			llm.User("Hello"),
		},
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.True(t, wantsExtendedCache(enriched), "CacheHint TTL=1h should request extended cache")

	body, err := ccBuildRequest(enriched)
	require.NoError(t, err)
	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	assert.Equal(t, "24h", req["prompt_cache_retention"])
}

func TestEnrichOpts_CacheHintTTL_DefaultDoesNotForceExtended(t *testing.T) {
	// CacheHint with no TTL should not trigger extended cache on a model
	// that doesn't support it.
	opts := llm.Request{
		Model: "gpt-4o-mini",
		Messages: llm.Messages{
			llm.User("Hello"),
		},
		CacheHint: &llm.CacheHint{Enabled: true}, // no TTL
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.False(t, wantsExtendedCache(enriched), "no TTL on non-extended model should not request 24h cache")

	body, err := ccBuildRequest(enriched)
	require.NoError(t, err)
	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	_, exists := req["prompt_cache_retention"]
	assert.False(t, exists)
}

func TestEnrichOpts_CacheHintDisabled_NoEffect(t *testing.T) {
	opts := llm.Request{
		Model: "gpt-4o",
		Messages: llm.Messages{
			llm.User("Hello"),
		},
		CacheHint: &llm.CacheHint{Enabled: false, TTL: "1h"},
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	// Disabled hint with TTL "1h" should not trigger extended cache
	assert.False(t, wantsExtendedCache(enriched), "disabled CacheHint should not request extended cache")

	body, err := ccBuildRequest(enriched)
	require.NoError(t, err)
	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	_, exists := req["prompt_cache_retention"]
	assert.False(t, exists)
}

func TestEnrichOpts_NoCacheHint_ModelBasedDetectionStillWorks(t *testing.T) {
	// Without CacheHint, model-based auto-detection should still request 24h
	// for models that support it.
	opts := llm.Request{
		Model: "gpt-5.1-codex", // known extended-cache model in registry
		Messages: llm.Messages{
			llm.User("Hello"),
		},
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.True(t, wantsExtendedCache(enriched), "extended-cache model should auto-detect 24h retention")
}
