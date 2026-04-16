package unified

import (
	"context"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
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

	accPub := newLLMToolAccumulator(pub)

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
