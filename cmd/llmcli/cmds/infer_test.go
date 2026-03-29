package cmds

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestBuildInferSpec_Default_NoDemoTools(t *testing.T) {
	spec := buildInferSpec("hi", "fast", "", "", "", false)

	require.Len(t, spec.Messages, 1)
	require.Equal(t, llm.RoleUser, spec.Messages[0].Role())
	require.Equal(t, "hi", spec.Messages[0].(llm.IsUserMsg).Content())
	require.Nil(t, spec.ToolChoice)
	require.Empty(t, spec.Tools)
	require.Empty(t, spec.ToolHandlers)
}

func TestBuildInferSpec_WithSystem_NoDemoTools(t *testing.T) {
	spec := buildInferSpec("hi", "fast", "you are concise", "", "", false)

	require.Len(t, spec.Messages, 2)
	require.Equal(t, llm.RoleSystem, spec.Messages[0].Role())
	require.Equal(t, "you are concise", spec.Messages[0].(llm.IsSystemMsg).Content())
	require.Equal(t, llm.RoleUser, spec.Messages[1].Role())
	require.Nil(t, spec.ToolChoice)
	require.Empty(t, spec.Tools)
}

func TestBuildInferSpec_DemoTools_EnablesDefaultPersonaAndRequiredTools(t *testing.T) {
	spec := buildInferSpec("hi", "fast", "", "", "", true)

	require.Len(t, spec.Messages, 2)
	require.Equal(t, llm.RoleSystem, spec.Messages[0].Role())
	require.Equal(t, llm.RoleUser, spec.Messages[1].Role())
	require.Len(t, spec.Tools, 2)
	require.NotNil(t, spec.ToolChoice)
	_, ok := spec.ToolChoice.(llm.ToolChoiceRequired)
	require.True(t, ok)
	require.Len(t, spec.ToolHandlers, 2)
}
