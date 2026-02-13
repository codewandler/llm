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

func (f *Provider) SendMessage(_ context.Context, _ llm.SendOptions) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 16)
	go func() {
		if !f.called {
			f.called = true
			ch <- llm.StreamEvent{
				Type: llm.StreamEventToolCall,
				ToolCall: &llm.ToolCall{
					Name:      "bash",
					ID:        "bash-1",
					Arguments: map[string]any{"command": "echo hello"},
				},
			}
		} else {
			ch <- llm.StreamEvent{Type: llm.StreamEventDelta, Delta: "done"}
		}
		ch <- llm.StreamEvent{Type: llm.StreamEventDone, Usage: &llm.Usage{InputTokens: 1, OutputTokens: 1, TotalTokens: 2, Cost: 0.01}}
		close(ch)
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
