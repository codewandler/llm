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
	fakeModelList = llm.Models{
		defaultModel,
	}
)

type Provider struct {
	called bool
}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Name() string       { return ProviderName }
func (p *Provider) Models() llm.Models { return fakeModelList }

var fakePricing = usage.Pricing{Input: 5.0, Output: 15.0}

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
func (p *Provider) CreateStream(_ context.Context, _ llm.Buildable) (llm.Stream, error) {
	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()

		pub.Started(llm.StreamStartedEvent{
			Model:     "fake-model-v1",
			RequestID: "fake-req-123",
			Provider:  "fake",
		})

		if !p.called {
			p.called = true
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
