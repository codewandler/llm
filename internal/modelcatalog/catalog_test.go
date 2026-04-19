package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalProvider(t *testing.T) {
	assert.Equal(t, "anthropic", CanonicalProvider("claude"))
	assert.Equal(t, "codex", CanonicalProvider("codex"))
	assert.Equal(t, "openai", BasisProvider("codex"))
	assert.Equal(t, "openai", CanonicalProvider("openai"))
	assert.Equal(t, "ollama", CanonicalProvider("ollama"))
}

func TestResolveWireModelIdentityFromCatalog_UsesCanonicalProvider(t *testing.T) {
	cat, err := LoadBuiltIn()
	require.NoError(t, err)

	identityAnthropic, okAnthropic := ResolveWireModelIdentityFromCatalog(cat, "anthropic", "claude-sonnet-4-6")
	require.True(t, okAnthropic)
	assert.Equal(t, "anthropic", identityAnthropic.Creator)
	assert.Equal(t, "claude", identityAnthropic.Family)

	identityClaude, okClaude := ResolveWireModelIdentityFromCatalog(cat, "claude", "claude-sonnet-4-6")
	require.True(t, okClaude)
	assert.Equal(t, identityAnthropic, identityClaude)

	identityCodex, okCodex := ResolveWireModelIdentityFromCatalog(cat, "codex", "gpt-5.4-mini")
	require.True(t, okCodex)
	assert.Equal(t, "openai", identityCodex.Creator)
	assert.Equal(t, "gpt", identityCodex.Family)
}

func TestResolveWireModelIdentityFromCatalog_UnknownModel(t *testing.T) {
	cat, err := LoadBuiltIn()
	require.NoError(t, err)

	identity, ok := ResolveWireModelIdentityFromCatalog(cat, "anthropic", "definitely-missing-model")
	assert.False(t, ok)
	assert.Equal(t, WireModelIdentity{}, identity)
}
