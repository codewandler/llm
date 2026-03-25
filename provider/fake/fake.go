package fake

import (
	"context"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

// Provider is a test-only provider that returns a single tool call
// on the first request and a text-only response on subsequent requests.
type Provider struct {
	called bool
}

func (f *Provider) Name() string { return "fake" }

func (f *Provider) CreateStream(_ context.Context, opts llm.Request) (llm.Stream, error) {
	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()

		pub.Started(llm.StreamStartedEvent{
			Model:     "fake-model-v1",
			RequestID: "fake-req-123",
		})

		if !f.called {
			f.called = true
			pub.ToolCall(tool.NewToolCall("bash-1", "bash", map[string]any{"command": "echo hello"}))
			pub.Usage(llm.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, Cost: 0.01})
			pub.Completed(llm.CompletedEvent{StopReason: llm.StopReasonToolUse})
		} else {
			pub.Delta(llm.TextDelta("done"))
			pub.Usage(llm.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, Cost: 0.01})
			pub.Completed(llm.CompletedEvent{StopReason: llm.StopReasonEndTurn})
		}
	}()
	return ch, nil
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
