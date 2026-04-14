package fake

import (
	"context"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

const (
	ProviderName      = "fake"
	Model1ID          = "fake/model-1"
	Model1DisplayName = "Fake Model 1"
)

var (
	defaultModel = llm.Model{
		ID:       Model1ID,
		Name:     Model1DisplayName,
		Provider: ProviderName,
		Aliases:  []string{llm.ModelDefault},
	}
	fakeModelList = []llm.Model{
		defaultModel,
	}
)

// Provider is a test-only provider that returns a single tool call
// on the first request and a text-only response on subsequent requests.
type Provider struct {
	called bool
}

// NewProvider returns a test-only provider.
func NewProvider(opts ...llm.ProviderOpt) llm.Provider {
	p := &Provider{}
	return llm.NewProvider(
		ProviderName,
		llm.WithStreamer(p),
		llm.WithModels(fakeModelList),
		llm.WithProviderOpts(opts...),
	)
}

func (f *Provider) CreateStream(_ context.Context, _ llm.Buildable) (llm.Stream, error) {
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
