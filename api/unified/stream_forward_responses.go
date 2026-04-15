package unified

import (
	"context"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/usage"
)

// ForwardResponses consumes a responses stream handle and forwards unified
// events into the current llm publisher path.
func ForwardResponses(
	ctx context.Context,
	handle *apicore.StreamHandle,
	pub llm.Publisher,
	sctx StreamContext,
) {
	_ = ctx

	var (
		requestID      string
		responseModel  string
		allTokens      usage.TokenItems
		stopReason     llm.StopReason
		sawToolUseLike bool
	)

	provider := sctx.Provider
	upstreamProvider := sctx.UpstreamProvider
	if upstreamProvider == "" {
		upstreamProvider = provider
	}

	for result := range handle.Events {
		if result.Err != nil {
			pub.Error(result.Err)
			return
		}

		uEv, ignored, err := MapResponsesEvent(result.Event)
		if err != nil {
			pub.Error(err)
			return
		}
		if ignored {
			continue
		}

		if uEv.Started != nil {
			requestID = uEv.Started.RequestID
			if uEv.Started.Model != "" {
				responseModel = uEv.Started.Model
			}
			if uEv.Started.Model != "" && uEv.Started.Model != sctx.Model {
				pub.ModelResolved(provider, sctx.Model, uEv.Started.Model)
			}
			uEv.Started.Provider = upstreamProvider
			if sctx.RateLimits != nil {
				uEv.Started.Extra = map[string]any{"rate_limits": sctx.RateLimits}
			}
		}

		if uEv.ToolDelta != nil || uEv.StreamToolCall != nil || uEv.ToolCall != nil {
			sawToolUseLike = true
		}

		if uEv.Usage != nil {
			allTokens = append(allTokens, uEv.Usage.Tokens...)
			uEv.Usage = nil
		}
		if uEv.Completed != nil {
			stopReason = uEv.Completed.StopReason
			if stopReason == llm.StopReasonEndTurn && sawToolUseLike {
				stopReason = llm.StopReasonToolUse
			}
			uEv.Completed = nil
		}

		if hasUnifiedPayload(uEv) {
			if err := PublishToLLM(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}

	emitUsageRecord(pub, sctx, provider, requestID, responseModel, allTokens.NonZero())
	pub.Completed(llm.CompletedEvent{StopReason: stopReason})
}
