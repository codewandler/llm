package unified

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// ForwardCompletions consumes a completions stream handle and forwards unified
// events into the current llm publisher path.
func ForwardCompletions(
	ctx context.Context,
	handle *apicore.StreamHandle,
	pub llm.Publisher,
	sctx StreamContext,
) {
	_ = ctx

	var (
		requestID     string
		responseModel string
		allTokens     usage.TokenItems
		stopReason    llm.StopReason
		startedOnce   bool
	)

	provider := sctx.Provider
	upstreamProvider := sctx.UpstreamProvider
	if upstreamProvider == "" {
		upstreamProvider = provider
	}

	accPub := newCompletionsToolAccumulator(pub)

	for result := range handle.Events {
		if result.Err != nil {
			accPub.Error(result.Err)
			return
		}

		uEv, ignored, err := MapCompletionsEvent(result)
		if err != nil {
			accPub.Error(err)
			return
		}
		if ignored {
			continue
		}

		if uEv.Started != nil && !startedOnce {
			startedOnce = true
			requestID = uEv.Started.RequestID
			if uEv.Started.Model != "" {
				responseModel = uEv.Started.Model
			}
			if uEv.Started.Model != "" && uEv.Started.Model != sctx.Model {
				accPub.ModelResolved(provider, sctx.Model, uEv.Started.Model)
			}
			uEv.Started.Provider = upstreamProvider
			if sctx.RateLimits != nil {
				uEv.Started.Extra = map[string]any{"rate_limits": sctx.RateLimits}
			}
		} else if uEv.Started != nil {
			uEv.Started = nil
		}

		if uEv.Usage != nil {
			allTokens = append(allTokens, uEv.Usage.Tokens...)
			uEv.Usage = nil
		}
		if uEv.Completed != nil {
			stopReason = uEv.Completed.StopReason
			uEv.Completed = nil
		}

		if hasUnifiedPayload(uEv) {
			if err := PublishToLLM(accPub, uEv); err != nil {
				accPub.Error(err)
				return
			}
		}
	}

	emitUsageRecord(accPub, sctx, provider, requestID, responseModel, allTokens.NonZero())
	accPub.Completed(llm.CompletedEvent{StopReason: stopReason})
}

type completionsToolAccumulator struct {
	llm.Publisher
	active map[uint32]*accumulatedCompletionTool
}

type accumulatedCompletionTool struct {
	id   string
	name string
	args strings.Builder
}

func newCompletionsToolAccumulator(base llm.Publisher) *completionsToolAccumulator {
	return &completionsToolAccumulator{
		Publisher: base,
		active:    make(map[uint32]*accumulatedCompletionTool),
	}
}

func (p *completionsToolAccumulator) Delta(ev *llm.DeltaEvent) {
	if ev != nil && ev.Kind == llm.DeltaKindTool && ev.Index != nil {
		idx := *ev.Index
		acc := p.active[idx]
		if acc == nil {
			acc = &accumulatedCompletionTool{}
			p.active[idx] = acc
		}
		if ev.ToolID != "" {
			acc.id = ev.ToolID
		}
		if ev.ToolName != "" {
			acc.name = ev.ToolName
		}
		if ev.ToolArgs != "" {
			acc.args.WriteString(ev.ToolArgs)
		}
	}
	p.Publisher.Delta(ev)
}

func (p *completionsToolAccumulator) Completed(ev llm.CompletedEvent) {
	if ev.StopReason == llm.StopReasonToolUse {
		p.flushToolCalls()
	}
	p.Publisher.Completed(ev)
}

func (p *completionsToolAccumulator) Close() {
	p.flushToolCalls()
	p.Publisher.Close()
}

func (p *completionsToolAccumulator) flushToolCalls() {
	if len(p.active) == 0 {
		return
	}

	indices := make([]uint32, 0, len(p.active))
	for idx := range p.active {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	for _, idx := range indices {
		acc := p.active[idx]
		if acc == nil {
			continue
		}
		if acc.args.Len() == 0 && acc.id == "" && acc.name == "" {
			continue
		}

		var args map[string]any
		if acc.args.Len() > 0 {
			if err := json.Unmarshal([]byte(acc.args.String()), &args); err != nil {
				args = map[string]any{"_raw": acc.args.String()}
			}
		}

		p.Publisher.ToolCall(tool.NewToolCall(acc.id, acc.name, args))
	}

	p.active = make(map[uint32]*accumulatedCompletionTool)
}
