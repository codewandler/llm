package anthropic

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeProvider_RealAPI_TextAndToolRoundtrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real API test in short mode")
	}

	path := defaultClaudeCredentialsPath()
	if path == "" {
		t.Skip("HOME not set for Claude Code credentials")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("Claude Code credentials not found at %s", path)
	}

	t.Run("text_response", func(t *testing.T) {
		p := NewClaudeCodeProvider()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		stream, err := p.CreateStream(ctx, llm.StreamOptions{
			Model: "sonnet",
			Messages: llm.Messages{
				&llm.UserMsg{Content: "Reply with exactly OK"},
			},
		})
		require.NoError(t, err)

		var out strings.Builder
		gotDone := false
		var doneUsage *llm.Usage
		for evt := range stream {
			switch evt.Type {
			case llm.StreamEventDelta:
				out.WriteString(evt.Delta)
			case llm.StreamEventError:
				require.NoError(t, evt.Error)
			case llm.StreamEventDone:
				gotDone = true
				doneUsage = evt.Usage
			}
		}

		require.True(t, gotDone, "expected done event from Claude Code provider")
		require.NotEmpty(t, strings.TrimSpace(out.String()), "expected non-empty response text")
		require.NotNil(t, doneUsage, "expected usage data in done event")
		assert.Greater(t, doneUsage.InputTokens, 0, "expected input token usage > 0")
		assert.Greater(t, doneUsage.OutputTokens, 0, "expected output token usage > 0")
		assert.Greater(t, doneUsage.TotalTokens, 0, "expected total token usage > 0")
	})

	t.Run("tool_call_roundtrip", func(t *testing.T) {
		type GetWeatherParams struct {
			Location string `json:"location" jsonschema:"description=City name,required"`
		}

		tools := []llm.ToolDefinition{
			llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get the current weather for a location"),
		}

		p := NewClaudeCodeProvider()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		firstPrompt := "What's the weather in Paris? You must call get_weather before answering."
		stream1, err := p.CreateStream(ctx, llm.StreamOptions{
			Model: "sonnet",
			Messages: llm.Messages{
				&llm.UserMsg{Content: firstPrompt},
			},
			Tools:      tools,
			ToolChoice: llm.ToolChoiceRequired{},
		})
		require.NoError(t, err)

		var toolCall *llm.ToolCall
		for evt := range stream1 {
			switch evt.Type {
			case llm.StreamEventToolCall:
				toolCall = evt.ToolCall
			case llm.StreamEventError:
				require.NoError(t, evt.Error)
			}
		}

		require.NotNil(t, toolCall, "expected tool call event")
		require.NotEmpty(t, toolCall.ID, "expected tool call id")
		assert.Equal(t, "get_weather", toolCall.Name)

		toolResultJSON := `{"temperature_c":22,"conditions":"sunny","location":"Paris"}`
		stream2, err := p.CreateStream(ctx, llm.StreamOptions{
			Model: "sonnet",
			Messages: llm.Messages{
				&llm.UserMsg{Content: firstPrompt},
				&llm.AssistantMsg{ToolCalls: []llm.ToolCall{{
					ID:        toolCall.ID,
					Name:      toolCall.Name,
					Arguments: toolCall.Arguments,
				}}},
				&llm.ToolCallResult{ToolCallID: toolCall.ID, Output: toolResultJSON},
			},
			Tools:      tools,
			ToolChoice: llm.ToolChoiceAuto{},
		})
		require.NoError(t, err)

		var answer strings.Builder
		gotDone := false
		var doneUsage *llm.Usage
		for evt := range stream2 {
			switch evt.Type {
			case llm.StreamEventDelta:
				answer.WriteString(evt.Delta)
			case llm.StreamEventError:
				require.NoError(t, evt.Error)
			case llm.StreamEventDone:
				gotDone = true
				doneUsage = evt.Usage
			}
		}

		require.True(t, gotDone, "expected done event after tool result")
		final := strings.ToLower(answer.String())
		assert.NotEmpty(t, strings.TrimSpace(final), "expected non-empty final answer")
		assert.True(t,
			strings.Contains(final, "paris") || strings.Contains(final, "22") || strings.Contains(final, "sunny"),
			"expected answer to reflect provided tool result, got: %q", answer.String(),
		)
		require.NotNil(t, doneUsage, "expected usage data in tool roundtrip done event")
		assert.Greater(t, doneUsage.InputTokens, 0)
		assert.Greater(t, doneUsage.OutputTokens, 0)
		assert.Greater(t, doneUsage.TotalTokens, 0)
	})
}

func TestClaudeCodeProvider_RealAPI_ModelMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real API test in short mode")
	}
	if os.Getenv("LLM_CLAUDE_MODEL_MATRIX") != "1" {
		t.Skip("set LLM_CLAUDE_MODEL_MATRIX=1 to run model matrix test")
	}

	path := defaultClaudeCredentialsPath()
	if path == "" {
		t.Skip("HOME not set for Claude Code credentials")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("Claude Code credentials not found at %s", path)
	}

	p := NewClaudeCodeProvider()
	models := p.Models()
	require.NotEmpty(t, models)

	var failures []string
	for _, model := range models {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		stream, err := p.CreateStream(ctx, llm.StreamOptions{
			Model: model.ID,
			Messages: llm.Messages{
				&llm.UserMsg{Content: "Reply with OK"},
			},
		})
		if err != nil {
			cancel()
			failures = append(failures, fmt.Sprintf("%s: create stream error: %v", model.ID, err))
			continue
		}

		gotDone := false
		gotUsage := false
		gotDelta := false
		for evt := range stream {
			switch evt.Type {
			case llm.StreamEventDelta:
				if strings.TrimSpace(evt.Delta) != "" {
					gotDelta = true
				}
			case llm.StreamEventDone:
				gotDone = true
				gotUsage = evt.Usage != nil && evt.Usage.TotalTokens > 0
			case llm.StreamEventError:
				failures = append(failures, fmt.Sprintf("%s: stream error: %v", model.ID, evt.Error))
			}
		}
		cancel()

		if !gotDone {
			failures = append(failures, fmt.Sprintf("%s: no done event", model.ID))
			continue
		}
		if !gotUsage {
			failures = append(failures, fmt.Sprintf("%s: missing usage in done event", model.ID))
		}
		if !gotDelta {
			failures = append(failures, fmt.Sprintf("%s: no text delta received", model.ID))
		}
	}

	if len(failures) > 0 {
		t.Fatalf("model matrix failures:\n%s", strings.Join(failures, "\n"))
	}
}
