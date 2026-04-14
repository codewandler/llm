package cmds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

func TestInferOpts_BuildMessages_NoDemoTools(t *testing.T) {
	opts := inferOpts{UserMsg: "hi", Model: "fast"}
	msgs := opts.buildMessages()

	require.Len(t, msgs, 1)
	assert.Equal(t, msg.RoleUser, msgs[0].Role)
}

func TestInferOpts_BuildMessages_WithSystem(t *testing.T) {
	opts := inferOpts{UserMsg: "hi", Model: "fast", System: "You are helpful."}
	msgs := opts.buildMessages()

	require.Len(t, msgs, 2)
	assert.Equal(t, msg.RoleSystem, msgs[0].Role)
	assert.Equal(t, msg.RoleUser, msgs[1].Role)
}

func TestInferOpts_BuildMessages_DemoToolsDefaultSystem(t *testing.T) {
	opts := inferOpts{UserMsg: "hi", Model: "fast", DemoTools: true}
	msgs := opts.buildMessages()

	require.Len(t, msgs, 2)
	assert.Equal(t, msg.RoleSystem, msgs[0].Role)
	assert.Contains(t, msgs[0].Text(), "Tessa")
	assert.Equal(t, msg.RoleUser, msgs[1].Role)
}

func TestBuildDemoTools(t *testing.T) {
	defs, handlers := buildDemoTools()

	require.Len(t, defs, 2)
	assert.Equal(t, "add_fact", defs[0].Name)
	assert.Equal(t, "complete_turn", defs[1].Name)

	require.Len(t, handlers, 2)
}

func TestInferOpts_ResolveToolChoice(t *testing.T) {
	tests := []struct {
		name string
		opts inferOpts
		want llm.ToolChoice
	}{
		{
			name: "no flags → nil",
			opts: inferOpts{},
			want: nil,
		},
		{
			name: "demo-tools only → ToolChoiceRequired",
			opts: inferOpts{DemoTools: true},
			want: llm.ToolChoiceRequired{},
		},
		{
			name: "demo-tools + explicit auto → ToolChoiceAuto (flag wins)",
			opts: inferOpts{
				DemoTools:  true,
				ToolChoice: llm.ToolChoiceFlag{Value: llm.ToolChoiceAuto{}},
			},
			want: llm.ToolChoiceAuto{},
		},
		{
			name: "demo-tools + explicit none → ToolChoiceNone (flag wins)",
			opts: inferOpts{
				DemoTools:  true,
				ToolChoice: llm.ToolChoiceFlag{Value: llm.ToolChoiceNone{}},
			},
			want: llm.ToolChoiceNone{},
		},
		{
			name: "explicit required without demo-tools → ToolChoiceRequired",
			opts: inferOpts{
				ToolChoice: llm.ToolChoiceFlag{Value: llm.ToolChoiceRequired{}},
			},
			want: llm.ToolChoiceRequired{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.opts.resolveToolChoice())
		})
	}
}
