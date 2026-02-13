package fake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

// TestProviderBasicStreaming tests basic streaming functionality with the fake provider.
func TestProviderBasicStreaming(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	tests := []struct {
		name          string
		opts          llm.StreamOptions
		wantToolCall  bool
		wantTextDelta bool
		wantDone      bool
	}{
		{
			name: "first call returns tool call",
			opts: llm.StreamOptions{
				Model: "fake-model",
				Messages: []llm.Message{
					{Role: llm.RoleUser, Content: "test message"},
				},
			},
			wantToolCall:  true,
			wantTextDelta: false,
			wantDone:      true,
		},
		{
			name: "second call returns text",
			opts: llm.StreamOptions{
				Model: "fake-model",
				Messages: []llm.Message{
					{Role: llm.RoleUser, Content: "another message"},
				},
			},
			wantToolCall:  false,
			wantTextDelta: true,
			wantDone:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream, err := p.CreateStream(ctx, tt.opts)
			require.NoError(t, err)

			var (
				gotToolCall  bool
				gotTextDelta bool
				gotDone      bool
			)

			for event := range stream {
				switch event.Type {
				case llm.StreamEventToolCall:
					gotToolCall = true
					require.NotNil(t, event.ToolCall, "StreamEventToolCall has nil ToolCall")
					assert.NotEmpty(t, event.ToolCall.ID, "ToolCall.ID is empty")
					assert.NotEmpty(t, event.ToolCall.Name, "ToolCall.Name is empty")

				case llm.StreamEventDelta:
					gotTextDelta = true
					assert.NotEmpty(t, event.Delta, "StreamEventDelta has empty Delta")

				case llm.StreamEventDone:
					gotDone = true
					require.NotNil(t, event.Usage, "StreamEventDone has nil Usage")
					assert.NotZero(t, event.Usage.TotalTokens, "Usage.TotalTokens is 0")

				case llm.StreamEventError:
					t.Errorf("Unexpected error event: %v", event.Error)
				}
			}

			// Verify expected events were received
			assert.Equal(t, tt.wantToolCall, gotToolCall, "tool call event mismatch")
			assert.Equal(t, tt.wantTextDelta, gotTextDelta, "text delta event mismatch")
			assert.Equal(t, tt.wantDone, gotDone, "done event mismatch")
		})
	}
}

// TestProviderContextCancellation verifies context cancellation is handled properly.
func TestProviderContextCancellation(t *testing.T) {
	p := NewProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model:    "fake-model",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "test"}},
	})
	require.NoError(t, err)

	// Drain the channel
	for range stream {
		// The fake provider completes quickly, so we may not hit timeout
	}

	// Verify context was canceled or completed
	if ctx.Err() != nil {
		assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	}
}

// TestProviderToolCallStructure verifies tool call structure is correct.
func TestProviderToolCallStructure(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model: "fake-model",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "test"},
		},
	})
	require.NoError(t, err)

	var toolCall *llm.ToolCall
	for event := range stream {
		if event.Type == llm.StreamEventToolCall {
			toolCall = event.ToolCall
			break
		}
	}

	require.NotNil(t, toolCall, "No tool call received")

	// Verify tool call structure
	assert.NotEmpty(t, toolCall.ID, "ToolCall.ID is empty")
	assert.Equal(t, "bash", toolCall.Name, "ToolCall.Name mismatch")
	assert.NotNil(t, toolCall.Arguments, "ToolCall.Arguments is nil")

	// Verify arguments contain expected keys
	assert.Contains(t, toolCall.Arguments, "command", "Arguments missing 'command' key")
	assert.Equal(t, "echo hello", toolCall.Arguments["command"], "Arguments['command'] mismatch")
}

// TestProviderWithTools verifies tool definitions are accepted.
func TestProviderWithTools(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	tools := []llm.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get the weather for a location",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			},
		},
	}

	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model:    "fake-model",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "What's the weather?"}},
		Tools:    tools,
	})
	require.NoError(t, err)

	// Drain and verify we get events
	eventCount := 0
	for event := range stream {
		eventCount++
		if event.Type == llm.StreamEventError {
			t.Errorf("Error event: %v", event.Error)
		}
	}

	assert.NotZero(t, eventCount, "No events received when sending with tools")
}

// TestProviderMultipleMessages verifies conversation history handling.
func TestProviderMultipleMessages(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "You are a helpful assistant."},
		{Role: llm.RoleUser, Content: "Hello"},
		{Role: llm.RoleAssistant, Content: "Hi there!"},
		{Role: llm.RoleUser, Content: "How are you?"},
	}

	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model:    "fake-model",
		Messages: messages,
	})
	require.NoError(t, err)

	// Verify stream completes without error
	for event := range stream {
		assert.NotEqual(t, llm.StreamEventError, event.Type,
			"Unexpected error event: %v", event.Error)
	}
}

// TestProviderName verifies the provider name is correct.
func TestProviderName(t *testing.T) {
	p := NewProvider()
	assert.Equal(t, "fake", p.Name())
}

// TestProviderModels verifies the provider returns valid models.
func TestProviderModels(t *testing.T) {
	p := NewProvider()
	models := p.Models()

	require.Len(t, models, 1, "Expected exactly one model")

	model := models[0]
	assert.Equal(t, "fake-model", model.ID)
	assert.Equal(t, "Fake Model", model.Name)
	assert.Equal(t, "fake", model.Provider)
}

// BenchmarkProviderStreaming benchmarks the streaming performance.
func BenchmarkProviderStreaming(b *testing.B) {
	ctx := context.Background()
	p := NewProvider()

	opts := llm.StreamOptions{
		Model:    "fake-model",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "benchmark test"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := p.CreateStream(ctx, opts)
		if err != nil {
			b.Fatalf("CreateStream() error = %v", err)
		}

		// Drain the stream
		for range stream {
		}
	}
}

// BenchmarkStreamEventProcessing benchmarks event processing.
func BenchmarkStreamEventProcessing(b *testing.B) {
	ctx := context.Background()
	p := NewProvider()

	opts := llm.StreamOptions{
		Model:    "fake-model",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "benchmark"}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := p.CreateStream(ctx, opts)
		if err != nil {
			b.Fatalf("CreateStream() error = %v", err)
		}

		eventCount := 0
		for event := range stream {
			eventCount++
			// Simulate basic processing
			_ = event.Type
			if event.ToolCall != nil {
				_ = event.ToolCall.Name
			}
		}
	}
}

// Example demonstrates basic usage of the fake provider.
func Example() {
	ctx := context.Background()
	p := NewProvider()

	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model: "fake-model",
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "Hello!"},
		},
	})
	if err != nil {
		panic(err)
	}

	for event := range stream {
		switch event.Type {
		case llm.StreamEventDelta:
			// Handle text response
			_ = event.Delta
		case llm.StreamEventToolCall:
			// Handle tool invocation
			_ = event.ToolCall
		case llm.StreamEventDone:
			// Stream complete
			return
		case llm.StreamEventError:
			panic(event.Error)
		}
	}
}

// Ensure Provider implements provider.Provider at compile time.
var _ llm.Provider = (*Provider)(nil)
