package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeKey(t *testing.T) {
	key := NormalizeKey(ModelKey{
		Creator:     " Anthropic ",
		Family:      "Claude",
		Series:      "Opus",
		Version:     "4.6",
		Variant:     " Pro ",
		ReleaseDate: "20251001",
	})

	assert.Equal(t, ModelKey{
		Creator:     "anthropic",
		Family:      "claude",
		Series:      "opus",
		Version:     "4.6",
		Variant:     "pro",
		ReleaseDate: "2025-10-01",
	}, key)
}

func TestLineAndReleaseIDs(t *testing.T) {
	key := ModelKey{
		Creator:     "anthropic",
		Family:      "claude",
		Series:      "opus",
		Version:     "4.6",
		ReleaseDate: "2025-10-01",
	}

	assert.True(t, IsRelease(key))
	assert.Equal(t, "anthropic/claude/opus/4.6", LineID(key))
	assert.Equal(t, "anthropic/claude/opus/4.6@2025-10-01", ReleaseID(key))
	assert.Equal(t, ModelKey{
		Creator: "anthropic",
		Family:  "claude",
		Series:  "opus",
		Version: "4.6",
	}, LineKey(key))
}

func TestReleaseIDForLineKeyIsEmpty(t *testing.T) {
	key := ModelKey{Creator: "openai", Family: "gpt", Version: "5.4", Variant: "mini"}

	assert.False(t, IsRelease(key))
	assert.Equal(t, "openai/gpt/5.4/mini", LineID(key))
	assert.Empty(t, ReleaseID(key))
}
