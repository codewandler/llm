package modelcatalog

import (
	"testing"

	modeldb "github.com/codewandler/modeldb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMergedBuiltIn_AddsCodexOfferings(t *testing.T) {
	cat, err := LoadMergedBuiltIn()
	require.NoError(t, err)
	_, ok := cat.Services["codex"]
	require.True(t, ok)
	off, ok := cat.OfferingByRef(offeringRef("codex", "gpt-5.4"))
	require.True(t, ok)
	assert.Contains(t, off.SupportedParameters, "reasoning")
	assert.Contains(t, off.SupportedParameters, "reasoning_effort")
}

func TestLoadMergedBuiltIn_PreservesOpenRouterOpenAIMetadataOverlay(t *testing.T) {
	cat, err := LoadMergedBuiltIn()
	require.NoError(t, err)
	off, ok := cat.OfferingByRef(offeringRef("openrouter", "openai/gpt-5.1"))
	require.True(t, ok)
	assert.Contains(t, off.SupportedParameters, "reasoning_effort")
	assert.Contains(t, off.SupportedParameters, "include_reasoning")
}

func offeringRef(serviceID, wireModelID string) modeldb.OfferingRef {
	return modeldb.OfferingRef{ServiceID: serviceID, WireModelID: wireModelID}
}
