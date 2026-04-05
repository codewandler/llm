package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolDefinitionFor_StrictMode_AdditionalPropertiesFalse(t *testing.T) {
	type SimpleParams struct {
		Name string `json:"name" jsonschema:"description=Name,required"`
		Age  int    `json:"age" jsonschema:"description=Age"`
	}

	tool := DefinitionFor[SimpleParams]("test", "test")

	// Top-level object MUST have additionalProperties: false for strict validation
	additionalProps, ok := tool.Parameters["additionalProperties"]
	require.True(t, ok, "additionalProperties must be present")
	assert.Equal(t, false, additionalProps, "additionalProperties should be false")
}
