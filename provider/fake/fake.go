package fake

import (
	"context"

	"github.com/codewandler/llm"
)

// Provider is a test-only provider that returns a single tool call
// on the first request and a text-only response on subsequent requests.
type Provider struct {
	called bool
}

func (f *Provider) Name() string { return "fake" }

func (f *Provider) CreateStream(_ context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
	stream := llm.NewEventStream()
	go func() {
		defer stream.Close()
		// Emit start event first
		stream.Start(llm.StreamStartOpts{
			Model:     "fake-model-v1",
			RequestID: "fake-req-123",
		})

		if !f.called {
			f.called = true
			stream.ToolCall(llm.ToolCall{
				Name:      "bash",
				ID:        "bash-1",
				Arguments: map[string]any{"command": "echo hello"},
			})
		} else {
			stream.Send(llm.StreamEvent{Type: llm.StreamEventDelta, Delta: "done"})
		}
		stream.Done(&llm.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, Cost: 0.01})
	}()
	return stream.C(), nil
}

// NewProvider returns a test-only provider.
func NewProvider() llm.Provider {
	return &Provider{}
}

func (f *Provider) Models() []llm.Model {
	return []llm.Model{
		{ID: "fake-model", Name: "Fake Model", Provider: "fake"},
	}
}
