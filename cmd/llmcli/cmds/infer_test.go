package cmds

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/msg"
)

func TestBuildInferSpec_Default_NoDemoTools(t *testing.T) {
	spec := buildInferSpec("hi", "fast", "", "", "", false)

	require.Len(t, spec.Messages, 1)
	require.Equal(t, msg.RoleUser, spec.Messages[0].Role)
}

func TestBuildInferSpec_WithSystem(t *testing.T) {
	spec := buildInferSpec("hi", "fast", "", "You are helpful.", "", false)

	// System prompt is added to the user message or handled separately
	require.NotEmpty(t, spec.Messages)
	require.Equal(t, msg.RoleUser, spec.Messages[0].Role)
}

func TestBuildInferSpec_WithDemoTools(t *testing.T) {
	spec := buildInferSpec("hi", "fast", "", "", "", true)

	require.NotEmpty(t, spec.Messages)
	require.NotEmpty(t, spec.Tools)
	require.NotNil(t, spec.ToolChoice)
}
