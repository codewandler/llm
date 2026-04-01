package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/provider/anthropic/claude"
)

// TestSmartCacheIntegration_Claude reproduces the HTTP 400 error when assistant messages
// with content blocks are incorrectly reconstructed for the Anthropic API.
//
// Run with:
//
//	go test -v -run TestSmartCacheIntegration ./provider/anthropic/claude/...
func TestSmartCacheIntegration_Claude(t *testing.T) {
	// Create Claude provider — uses local token provider by default (Claude Code CLI)
	provider := claude.New()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// ---------------------------------------------------------------------------
	// Test 1: Simple request — baseline without smart cache
	// ---------------------------------------------------------------------------
	t.Run("SimpleRequest", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 100,
			Messages: []llm.Message{
				llm.User("say 'ok' in exactly one word"),
			},
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			// Print the error body for debugging
			var perr *llm.ProviderError
			if errors.As(err, &perr) {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 2: Multi-message with assistant last — exercises assistant reconstruction
	// ---------------------------------------------------------------------------
	t.Run("AssistantLast", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 100,
			Messages: []llm.Message{
				llm.User("list markdown files"),
				llm.Assistant("I'll run ls for you."),
			},
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			// Print the error body for debugging
			if perr, ok := err.(*llm.ProviderError); ok {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 3: Assistant message with blocks (thinking) — tests cache hint on thinking block
	// ---------------------------------------------------------------------------
	t.Run("AssistantWithThinkingBlocks", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 200,
			Messages: msg.BuildTranscript(
				llm.User("list *.md files"),
				msg.Assistant(
					//msg.Thinking("I need to run ls", "sig123"),
					msg.Text("I'll run ls *.md for you."),
				).Cache(),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			// Print the error body for debugging
			if perr, ok := err.(*llm.ProviderError); ok {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		require.NotEmpty(t, text)
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 4: Assistant with tool calls + cache hint — THIS IS THE BUG
	// ---------------------------------------------------------------------------
	t.Run("AssistantWithToolCallsAndCacheHint", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 200,
			Messages: msg.BuildTranscript(
				llm.User("list all markdown files"),
				msg.Assistant(
					msg.Text("I'll run ls for you."),
					msg.ToolCall{ID: "tc1", Name: "bash", Args: msg.ToolArgs{"cmd": "ls *.md"}},
				).Cache(),
				msg.Tool().Results(msg.ToolResult{ToolCallID: "tc1", ToolOutput: "file1.md\nfile2.md"}),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			if perr, ok := err.(*llm.ProviderError); ok {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error with tool calls + cache hint")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		require.NotEmpty(t, text)
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 5: Tool call sequence with cache hint on intermediate assistant
	// Simulates: User asks → Assistant uses tools → Tools execute → User asks again → Assistant responds (cached)
	// ---------------------------------------------------------------------------
	t.Run("ToolCallSequenceWithCacheHint", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 200,
			Messages: msg.BuildTranscript(
				llm.User("list markdown files"),
				msg.Assistant(
					msg.Text("I'll run ls for you."),
					msg.ToolCall{ID: "tc1", Name: "bash", Args: msg.ToolArgs{"cmd": "ls *.md"}},
				).Cache(),
				msg.Tool().Results(msg.ToolResult{ToolCallID: "tc1", ToolOutput: "file1.md\nfile2.md"}),
				llm.User("now count them"),
				msg.Assistant(msg.Text("There are 2 markdown files.")).Cache(),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			var perr *llm.ProviderError
			if errors.As(err, &perr) {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error with tool call sequence + cache hint")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		require.NotEmpty(t, text)
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 6: Multiple tool calls with cache hint
	// ---------------------------------------------------------------------------
	t.Run("MultipleToolCallsWithCacheHint", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 200,
			Messages: msg.BuildTranscript(
				llm.User("list markdown files and count them"),
				msg.Assistant(
					msg.Text("I'll do both."),
					msg.ToolCall{ID: "tc1", Name: "bash", Args: msg.ToolArgs{"cmd": "ls *.md"}},
					msg.ToolCall{ID: "tc2", Name: "bash", Args: msg.ToolArgs{"cmd": "ls *.md | wc -l"}},
				).Cache(),
				msg.Tool().Results(msg.ToolResult{ToolCallID: "tc1", ToolOutput: "file1.md\nfile2.md"}),
				msg.Tool().Results(msg.ToolResult{ToolCallID: "tc2", ToolOutput: "2"}),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			if perr, ok := err.(*llm.ProviderError); ok {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error with multiple tool calls + cache hint")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		require.NotEmpty(t, text)
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 7: Thought + Tool calls (no text) — THE BUG
	// When the last assistant message has thinking + tool_calls (no text), filtering
	// thinking leaves only tool_calls. Then we try to apply cache hint to the last
	// block (tool_calls) which is invalid → HTTP 400.
	// ---------------------------------------------------------------------------
	t.Run("ThinkingPlusToolCalls_NoText", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 200,
			Messages: msg.BuildTranscript(
				llm.User("list markdown files"),
				// This is the problematic case: thinking + tool_calls, no text
				msg.Assistant(
					//msg.Thinking("I should run ls to check", "sig-thinking-123"),
					msg.ToolCall{ID: "tc1", Name: "bash", Args: msg.ToolArgs{"cmd": "ls *.md"}},
				).Cache(),
				msg.Tool().Results(msg.ToolResult{ToolCallID: "tc1", ToolOutput: "file1.md\nfile2.md"}),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			var perr *llm.ProviderError
			if errors.As(err, &perr) {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error with thinking+tool_calls (no text)")
		}

		var text string
		for env := range stream {
			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text
				}
			}
		}
		require.NotEmpty(t, text)
		t.Logf("Response: %s", text)
	})

	// ---------------------------------------------------------------------------
	// Test 8: Assistant (no cache hint) with thinking + tool_calls
	// This simulates re-sending an assistant message from conversation history.
	// ---------------------------------------------------------------------------
	t.Run("Assistant_NoCacheHint", func(t *testing.T) {
		req := llm.Request{
			Model:          claude.ModelSonnet,
			MaxTokens:      200,
			ThinkingEffort: llm.ThinkingEffortHigh,
			OutputEffort:   llm.OutputEffortHigh,
			Messages: msg.BuildTranscript(
				llm.User("think about a blue elephant and then tell me in one word which color the elephant is"),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		if err != nil {
			var perr *llm.ProviderError
			if errors.As(err, &perr) {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error with assistant (no cache hint)")
		}

		res := llm.ProcessEvents(t.Context(), stream)

		/*var text string

		for env := range stream {

			k, _ := json.MarshalIndent(env.Data, "", "  ")
			t.Logf("Stream Event: %+v\n%+v\n", env, string(k))

			if env.Type == llm.StreamEventDelta {
				if delta, ok := env.Data.(*llm.DeltaEvent); ok {
					text += delta.Text

				}
			}
		}
		require.NotEmpty(t, text)*/
		r, _ := json.MarshalIndent(map[string]any{
			"text":        res.Text(),
			"think":       res.Thought(),
			"usage":       res.Usage(),
			"stop_reason": res.StopReason(),
			"next":        res.Next(),
		}, "", "  ")
		t.Logf("Response: %s", r)

		req2 := llm.Request{
			Model:          "sonnet",
			MaxTokens:      200,
			ThinkingEffort: llm.ThinkingEffortHigh,
			OutputEffort:   llm.OutputEffortHigh,
			Messages: msg.BuildTranscript(
				llm.User("think about a blue elephant and then tell me in one word which color the elephant is"),
				res.Next(),
				llm.User("what do you mean?"),
			),
		}

		stream, err = provider.CreateStream(ctx, req2)
		if err != nil {
			var perr *llm.ProviderError
			if errors.As(err, &perr) {
				t.Logf("API Error Body: %s", perr.ResponseBody)
			}
			require.NoError(t, err, "CreateStream() should not error with assistant (no cache hint)")
		}

		res = llm.ProcessEvents(t.Context(), stream)
		r, _ = json.MarshalIndent(map[string]any{
			"text":        res.Text(),
			"think":       res.Thought(),
			"usage":       res.Usage(),
			"stop_reason": res.StopReason(),
			"next":        res.Next(),
		}, "", "  ")
		t.Logf("Response 2: %s", r)
	})

	// ---------------------------------------------------------------------------
	// Test 10: Empty content blocks array with cache hint — expected to fail validation
	// An assistant message with empty contentBlocks AND no toolCalls is invalid.
	// ---------------------------------------------------------------------------
	t.Run("EmptyContentBlocks_WithCacheHint_ValidationError", func(t *testing.T) {
		req := llm.Request{
			MaxTokens: 200,
			Messages: msg.BuildTranscript(
				llm.User("list markdown files"),
				// Empty blocks + cache hint + no tool_calls → invalid message
				msg.Assistant().Cache(),
			),
		}

		stream, err := provider.CreateStream(ctx, req)
		// Validation error is expected: empty content + no tool_calls
		if err != nil {
			t.Logf("Expected validation error: %v", err)
		}
		// This is expected to fail at validation level, not API level
		if stream != nil {
			for range stream {
			}
		}
	})
}
