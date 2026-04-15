package fake

import (
	"context"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
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

// fakePricing is used to generate non-zero cost on fake usage records so that
// consumers testing cost-conditional display paths (e.g. printUsageRecord)
// exercise the cost branch without requiring a real provider.
var fakePricing = usage.Pricing{Input: 5.0, Output: 15.0} // arbitrary; matches Sonnet-class rates

func fakeUsageRecord() usage.Record {
	tokens := usage.TokenItems{
		{Kind: usage.KindInput, Count: 1},
		{Kind: usage.KindOutput, Count: 1},
	}
	return usage.Record{
		Dims:       usage.Dims{Provider: ProviderName, Model: "fake-model-v1"},
		Tokens:     tokens,
		Cost:       usage.CalcCost(tokens, fakePricing),
		RecordedAt: time.Now(),
	}
}
func (f *Provider) CreateStream(_ context.Context, _ llm.Buildable) (llm.Stream, error) {
	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()

		pub.Started(llm.StreamStartedEvent{
			Model:     "fake-model-v1",
			RequestID: "fake-req-123",
			Provider:  "fake",
		})

		if !f.called {
			f.called = true
			pub.ToolCall(tool.NewToolCall("bash-1", "bash", map[string]any{"command": "echo hello"}))
			pub.UsageRecord(fakeUsageRecord())
			pub.Completed(llm.CompletedEvent{StopReason: llm.StopReasonToolUse})
		} else {
			pub.Delta(llm.TextDelta("done"))
			pub.UsageRecord(fakeUsageRecord())
			pub.Completed(llm.CompletedEvent{StopReason: llm.StopReasonEndTurn})
		}
	}()
	return ch, nil
}
