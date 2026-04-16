package unified

import (
	"context"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/usage"
)

// ForwardMessages consumes a messages stream handle and forwards unified events
// into the current llm publisher path.
func ForwardMessages(
	ctx context.Context,
	handle *apicore.StreamHandle,
	pub llm.Publisher,
	sctx StreamContext,
) {
	_ = ctx

	var (
		requestID     string
		responseModel string
		inputTokens   usage.TokenItems
		outputTokens  usage.TokenItems
		stopReason    llm.StopReason
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

		uEv, ignored, err := MapMessagesEvent(result)
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

		if uEv.Started != nil && uEv.Usage != nil {
			inputTokens = uEv.Usage.Tokens
			uEv.Usage = nil
		}
		if uEv.Completed != nil {
			if uEv.Usage != nil {
				outputTokens = uEv.Usage.Tokens
				uEv.Usage = nil
			}
			stopReason = uEv.Completed.StopReason
			uEv.Completed = nil
		}

		if hasUnifiedPayload(uEv) {
			if err := PublishToLLM(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}

	emitUsageRecord(pub, sctx, provider, requestID, responseModel, append(inputTokens, outputTokens...).NonZero())
	pub.Completed(llm.CompletedEvent{StopReason: stopReason})
}
