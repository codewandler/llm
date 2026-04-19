package modelcatalog

import (
	"testing"

	modeldb "github.com/codewandler/modeldb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMergedBuiltIn_ContainsCodexResponsesExposure(t *testing.T) {
	cat, err := LoadMergedBuiltIn()
	require.NoError(t, err)
	off, ok := cat.OfferingByRef(modeldb.OfferingRef{ServiceID: "codex", WireModelID: "gpt-5.4"})
	require.True(t, ok)
	exp := off.Exposure(modeldb.APITypeOpenAIResponses)
	require.NotNil(t, exp)
	require.NotNil(t, exp.ExposedCapabilities)
	require.NotNil(t, exp.ExposedCapabilities.Reasoning)
	assert.True(t, exp.ExposedCapabilities.Reasoning.Available)
	assert.True(t, exp.SupportsParameter(modeldb.ParamReasoningEffort))
	assert.True(t, exp.SupportsParameter(modeldb.ParamReasoningSummary))
	assert.True(t, exp.SupportsParameterValue(string(modeldb.ParamReasoningSummary), string(modeldb.ReasoningSummaryAuto)))
}

func TestLoadMergedBuiltIn_ContainsOpenAIResponsesReasoningExposure(t *testing.T) {
	cat, err := LoadMergedBuiltIn()
	require.NoError(t, err)
	for _, wireModelID := range []string{"gpt-5.1", "gpt-5.4"} {
		off, ok := cat.OfferingByRef(modeldb.OfferingRef{ServiceID: "openai", WireModelID: wireModelID})
		require.True(t, ok, wireModelID)
		exp := off.Exposure(modeldb.APITypeOpenAIResponses)
		require.NotNil(t, exp, wireModelID)
		require.NotNil(t, exp.ExposedCapabilities, wireModelID)
		require.NotNil(t, exp.ExposedCapabilities.Reasoning, wireModelID)
		assert.True(t, exp.ExposedCapabilities.Reasoning.Available, wireModelID)
		assert.True(t, exp.SupportsParameter(modeldb.ParamReasoningEffort), wireModelID)
		assert.True(t, exp.SupportsParameter(modeldb.ParamReasoningSummary), wireModelID)
	}
}
