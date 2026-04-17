package fake

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

func TestProvider_ResolveDefaultModel(t *testing.T) {
	p := NewProvider()
	m, err := p.Models().Resolve(llm.ModelDefault)
	require.NoError(t, err)
	assert.Equal(t, Model1ID, m.ID)
}

func TestProviderBasicStreaming(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	tests := []struct {
		name          string
		opts          llm.Request
		wantToolCall  bool
		wantTextDelta bool
		wantDone      bool
	}{
		{
			name: "first call returns tool call",
			opts: llm.Request{
				Model: "fake-model",
				Messages: llm.Messages{
					llm.User("test message"),
				},
			},
			wantToolCall:  true,
			wantTextDelta: false,
			wantDone:      true,
		},
		{
			name: "second call returns text",
			opts: llm.Request{
				Model: "fake-model",
				Messages: llm.Messages{
					llm.User("another message"),
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
					tc, ok := event.Data.(*llm.ToolCallEvent)
					require.True(t, ok, "StreamEventToolCall has nil ToolCallEvent")
					assert.NotEmpty(t, tc.ToolCall.ToolCallID(), "ToolCallEvent.ToolCallID is empty")
					assert.NotEmpty(t, tc.ToolCall.ToolName(), "ToolCallEvent.ToolName is empty")

				case llm.StreamEventDelta:
					gotTextDelta = true

				case llm.StreamEventCompleted:
					gotDone = true

				case llm.StreamEventError:
					t.Errorf("Unexpected error event: %v", event.Data)
				}
			}

			assert.Equal(t, tt.wantToolCall, gotToolCall, "tool call event mismatch")
			assert.Equal(t, tt.wantTextDelta, gotTextDelta, "text delta event mismatch")
			assert.Equal(t, tt.wantDone, gotDone, "done event mismatch")
		})
	}
}

func TestProviderContextCancellation(t *testing.T) {
	p := NewProvider()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    "fake-model",
		Messages: llm.Messages{llm.User("test")},
	})
	require.NoError(t, err)

	for range stream {
	}

	if ctx.Err() != nil {
		assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	}
}

func TestProviderToolCallStructure(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    "fake-model",
		Messages: llm.Messages{llm.User("test")},
	})
	require.NoError(t, err)

	var toolCall tool.Call
	for event := range stream {
		if event.Type == llm.StreamEventToolCall {
			tc, ok := event.Data.(*llm.ToolCallEvent)
			require.True(t, ok, "StreamEventToolCall has nil ToolCallEvent")
			toolCall = tc.ToolCall
			break
		}
	}

	require.NotNil(t, toolCall, "No tool call received")

	assert.NotEmpty(t, toolCall.ToolCallID(), "ToolCall.ToolCallID is empty")
	assert.Equal(t, "bash", toolCall.ToolName(), "ToolCall.ToolName mismatch")

	args := toolCall.ToolArgs()
	assert.NotNil(t, args, "ToolCall.ToolArgs is nil")
	assert.Contains(t, args, "command", "ToolArgs missing 'command' key")
}

func TestProviderWithTools(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	tools := []tool.Definition{
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

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    "fake-model",
		Messages: llm.Messages{llm.User("What's the weather?")},
		Tools:    tools,
	})
	require.NoError(t, err)

	eventCount := 0
	for event := range stream {
		eventCount++
		if event.Type == llm.StreamEventError {
			t.Errorf("Error event: %v", event.Data)
		}
	}

	assert.NotZero(t, eventCount, "No events received when sending with tools")
}

func TestProviderMultipleMessages(t *testing.T) {
	ctx := context.Background()
	p := NewProvider()

	messages := llm.Messages{
		llm.System("You are a helpful assistant."),
		llm.User("Hello"),
		llm.Assistant("Hi there!"),
		llm.User("How are you?"),
	}

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    "fake-model",
		Messages: messages,
	})
	require.NoError(t, err)

	for event := range stream {
		assert.NotEqual(t, llm.StreamEventError, event.Type,
			"Unexpected error event: %v", event.Data)
	}
}

func TestProviderName(t *testing.T) {
	p := NewProvider()
	assert.Equal(t, "fake", p.Name())
}

func TestProviderModels(t *testing.T) {
	p := NewProvider()
	models := p.Models()

	require.Len(t, models, 1, "Expected exactly one model")

	model := models[0]
	assert.Equal(t, "fake/model-1", model.ID)
	assert.Equal(t, "Fake Model 1", model.Name)
	assert.Equal(t, "fake", model.Provider)
}

func BenchmarkProviderStreaming(b *testing.B) {
	ctx := context.Background()
	p := NewProvider()

	opts := llm.Request{
		Model:    "fake-model",
		Messages: llm.Messages{llm.User("benchmark test")},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream, err := p.CreateStream(ctx, opts)
		if err != nil {
			b.Fatalf("CreateStream() error = %v", err)
		}

		for range stream {
		}
	}
}

func BenchmarkStreamEventProcessing(b *testing.B) {
	ctx := context.Background()
	p := NewProvider()

	opts := llm.Request{
		Model:    "fake-model",
		Messages: llm.Messages{llm.User("benchmark")},
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
			_ = event.Type
			if tc, ok := event.Data.(*llm.ToolCallEvent); ok {
				_ = tc.ToolCall.ToolName()
			}
		}
	}
}

func Example() {
	ctx := context.Background()
	p := NewProvider()

	stream, err := p.CreateStream(ctx, llm.Request{
		Model:    "fake-model",
		Messages: llm.Messages{llm.User("Hello!")},
	})
	if err != nil {
		panic(err)
	}

	for event := range stream {
		switch event.Type {
		case llm.StreamEventDelta:
			_ = event.Data
		case llm.StreamEventToolCall:
			_ = event.Data
		case llm.StreamEventCompleted:
			return
		case llm.StreamEventError:
			panic(event.Data)
		}
	}
}

func TestCreateStream_AcceptsBuilder(t *testing.T) {
	// Verify that *RequestBuilder satisfies llm.Buildable and can be passed
	// directly to CreateStream without an explicit Build() call at the call site.
	p := NewProvider()
	b := llm.NewRequestBuilder().
		Model(llm.ModelDefault).
		User("hello")
	_, err := p.CreateStream(context.Background(), b)
	require.NoError(t, err)
}
