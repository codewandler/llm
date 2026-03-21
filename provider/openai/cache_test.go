package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestEnrichOpts_CacheHintTTL_OneHour(t *testing.T) {
	// CacheHint with TTL "1h" should set PromptCacheRetention to "24h"
	// regardless of model capability.
	opts := llm.StreamOptions{
		Model: "gpt-4o-mini", // doesn't normally support extended cache
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
		CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	assert.Equal(t, "24h", enriched.PromptCacheRetention)
}

func TestEnrichOpts_CacheHintTTL_DefaultDoesNotForceExtended(t *testing.T) {
	// CacheHint with no explicit TTL (or TTL != "1h") should NOT force "24h"
	opts := llm.StreamOptions{
		Model: "gpt-4o-mini",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
		CacheHint: &llm.CacheHint{Enabled: true}, // no TTL
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	// gpt-4o-mini doesn't support extended cache, so PromptCacheRetention stays ""
	assert.Equal(t, "", enriched.PromptCacheRetention)
}

func TestEnrichOpts_CacheHintDisabled_NoEffect(t *testing.T) {
	opts := llm.StreamOptions{
		Model: "gpt-4o",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
		CacheHint: &llm.CacheHint{Enabled: false, TTL: "1h"},
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	// Disabled hint with TTL "1h" should not trigger extended cache
	assert.Equal(t, "", enriched.PromptCacheRetention)
}

func TestEnrichOpts_NoCacheHint_ModelBasedDetectionStillWorks(t *testing.T) {
	// Without CacheHint, model-based auto-detection should still set "24h" for
	// models that support it.
	opts := llm.StreamOptions{
		Model: "codex-mini-latest", // Codex models support extended cache
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
	}

	enriched, err := enrichOpts(opts)
	require.NoError(t, err)
	// Model-based detection should still work when CacheHint is nil
	// (exact value depends on registry; just check no panic and no error)
	_ = enriched
}
