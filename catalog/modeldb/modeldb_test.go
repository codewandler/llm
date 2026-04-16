package modeldb

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	db, err := Load()
	require.NoError(t, err)
	assert.NotEmpty(t, db)

	// Check that we have a reasonable number of providers
	assert.Greater(t, len(db), 10)
}

func TestLoad_Bedrock(t *testing.T) {
	db, err := Load()
	require.NoError(t, err)

	bedrock, ok := db["amazon-bedrock"]
	require.True(t, ok, "amazon-bedrock provider should exist")
	assert.Equal(t, "amazon-bedrock", bedrock.ID)
	assert.Equal(t, "Amazon Bedrock", bedrock.Name)
	assert.NotEmpty(t, bedrock.Models)

	// Check a known Claude model
	claude, ok := bedrock.Models["anthropic.claude-3-5-haiku-20241022-v1:0"]
	require.True(t, ok, "claude-3-5-haiku model should exist")
	assert.Contains(t, claude.Name, "Claude")
	assert.True(t, claude.ToolCall)
	assert.Greater(t, claude.Cost.Input, 0.0)
	assert.Greater(t, claude.Cost.Output, 0.0)
}

func TestLoad_Anthropic(t *testing.T) {
	db, err := Load()
	require.NoError(t, err)

	anthropic, ok := db["anthropic"]
	require.True(t, ok, "anthropic provider should exist")
	assert.Equal(t, "Anthropic", anthropic.Name)
	assert.NotEmpty(t, anthropic.Models)
}

func TestLoad_OpenAI(t *testing.T) {
	db, err := Load()
	require.NoError(t, err)

	openai, ok := db["openai"]
	require.True(t, ok, "openai provider should exist")
	assert.NotEmpty(t, openai.Models)
}

func TestGetProvider(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantID     string
		wantExists bool
	}{
		{
			name:       "bedrock maps to amazon-bedrock",
			input:      "bedrock",
			wantID:     "amazon-bedrock",
			wantExists: true,
		},
		{
			name:       "anthropic passes through",
			input:      "anthropic",
			wantID:     "anthropic",
			wantExists: true,
		},
		{
			name:       "direct amazon-bedrock also works",
			input:      "amazon-bedrock",
			wantID:     "amazon-bedrock",
			wantExists: true,
		},
		{
			name:       "unknown provider",
			input:      "nonexistent-provider",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, ok := GetProvider(tt.input)
			assert.Equal(t, tt.wantExists, ok)
			if tt.wantExists {
				assert.Equal(t, tt.wantID, p.ID)
			}
		})
	}
}

func TestGetModel(t *testing.T) {
	// Test with mapped provider name
	model, ok := GetModel("bedrock", "amazon.nova-micro-v1:0")
	require.True(t, ok)
	assert.Contains(t, model.Name, "Nova")
	assert.Greater(t, model.Limit.Context, 0)

	// Test non-existent model
	_, ok = GetModel("bedrock", "nonexistent-model")
	assert.False(t, ok)

	// Test non-existent provider
	_, ok = GetModel("nonexistent", "some-model")
	assert.False(t, ok)
}

func TestProviders(t *testing.T) {
	providers := Providers()
	assert.NotEmpty(t, providers)
	assert.Contains(t, providers, "amazon-bedrock")
	assert.Contains(t, providers, "anthropic")
	assert.Contains(t, providers, "openai")
}

func TestInterleaved_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantEnabled bool
		wantField   string
	}{
		{
			name:        "boolean true",
			input:       "true",
			wantEnabled: true,
			wantField:   "",
		},
		{
			name:        "boolean false",
			input:       "false",
			wantEnabled: false,
			wantField:   "",
		},
		{
			name:        "object with field",
			input:       `{"field":"reasoning_content"}`,
			wantEnabled: true,
			wantField:   "reasoning_content",
		},
		{
			name:        "object with different field",
			input:       `{"field":"reasoning_details"}`,
			wantEnabled: true,
			wantField:   "reasoning_details",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var i Interleaved
			err := json.Unmarshal([]byte(tt.input), &i)
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnabled, i.Enabled)
			assert.Equal(t, tt.wantField, i.Field)
		})
	}
}

func TestInterleaved_MarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		input Interleaved
		want  string
	}{
		{
			name:  "enabled without field",
			input: Interleaved{Enabled: true},
			want:  "true",
		},
		{
			name:  "disabled",
			input: Interleaved{Enabled: false},
			want:  "false",
		},
		{
			name:  "with field",
			input: Interleaved{Enabled: true, Field: "reasoning_content"},
			want:  `{"field":"reasoning_content"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

func TestCost_ContextOver200k(t *testing.T) {
	db, err := Load()
	require.NoError(t, err)

	// Find a model with context_over_200k pricing (Claude models have this)
	bedrock := db["amazon-bedrock"]
	model, ok := bedrock.Models["us.anthropic.claude-opus-4-6-v1"]
	if !ok {
		t.Skip("Model us.anthropic.claude-opus-4-6-v1 not found")
	}

	require.NotNil(t, model.Cost.ContextOver200k)
	assert.Greater(t, model.Cost.ContextOver200k.Input, model.Cost.Input)
	assert.Greater(t, model.Cost.ContextOver200k.Output, model.Cost.Output)
}

func TestModalities(t *testing.T) {
	db, err := Load()
	require.NoError(t, err)

	// Find a multimodal model
	bedrock := db["amazon-bedrock"]
	for _, model := range bedrock.Models {
		if len(model.Modalities.Input) > 1 {
			assert.Contains(t, model.Modalities.Input, "text")
			// Should have image or other modality
			hasOther := false
			for _, m := range model.Modalities.Input {
				if m != "text" {
					hasOther = true
					break
				}
			}
			assert.True(t, hasOther, "multimodal model should have non-text input")
			return
		}
	}
	t.Log("No multimodal models found in bedrock, skipping detailed check")
}

func TestMustLoad(t *testing.T) {
	// Should not panic
	assert.NotPanics(t, func() {
		db := MustLoad()
		assert.NotEmpty(t, db)
	})
}
