package unified

import (
	"context"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/usage"
)

// StreamContext carries provider metadata used to enrich unified events before
// publishing. Populate it with what is known before the stream starts; the
// RequestID field may be left empty and will be filled from the first Started
// event in the stream.
type StreamContext struct {
	// Provider is the provider name used in usage records and stream events
	// (e.g. "anthropic", "openai", "openrouter").
	Provider string

	// Model is the model ID that was requested. If the API resolves a
	// different model, ModelResolved will be emitted automatically.
	Model string

	// UpstreamProvider is the upstream backend provider when routing through
	// a proxy (e.g. openrouter routes to "anthropic"). When empty, Provider
	// is used.
	UpstreamProvider string

	// CostCalc calculates token cost. When nil, cost is omitted.
	CostCalc usage.CostCalculator

	// RateLimits from response headers. Set after the HTTP response is received.
	RateLimits *llm.RateLimits
}

// StreamMessages consumes a messages.StreamHandle and publishes canonical
// events to pub. It enriches usage records with provider dims, handles model
// resolution, and calculates cost via sctx.CostCalc.
//
// The caller must close pub after StreamMessages returns (or defer pub.Close
// before calling in a goroutine). StreamMessages does NOT close pub itself
// so callers can compose multiple stream phases.
func StreamMessages(
	ctx context.Context,
	handle *apicore.StreamHandle,
	pub llm.Publisher,
	sctx StreamContext,
) {
	_ = ctx // reserved for future context-aware cancellation hooks

	var (
		requestID    string
		inputTokens  usage.TokenItems
		outputTokens usage.TokenItems
		stopReason   llm.StopReason
	)

	provider := sctx.Provider
	upstreamProvider := sctx.UpstreamProvider
	if upstreamProvider == "" {
		upstreamProvider = provider
	}

	if sctx.RateLimits != nil {
		// Rate limits are already available from response headers before streaming.
		// They will be included in the Started Extra map on first event.
	}

	for result := range handle.Events {
		if result.Err != nil {
			pub.Error(result.Err)
			return
		}

		uEv, ignored, err := EventFromMessages(result.Event)
		if err != nil {
			pub.Error(err)
			return
		}
		if ignored {
			continue
		}

		// Enrich Started event with rate limits, upstream provider, and model resolution.
		if uEv.Started != nil {
			requestID = uEv.Started.RequestID

			if uEv.Started.Model != "" && uEv.Started.Model != sctx.Model {
				pub.ModelResolved(provider, sctx.Model, uEv.Started.Model)
			}
			uEv.Started.Provider = upstreamProvider
			if sctx.RateLimits != nil {
				uEv.Started.Extra = map[string]any{"rate_limits": sctx.RateLimits}
			}
		}

		// Accumulate usage tokens across events; emit a combined record at the end.
		// MessageStartEvent carries input tokens alongside Started.
		if uEv.Started != nil && uEv.Usage != nil {
			inputTokens = uEv.Usage.Tokens
			uEv.Usage = nil // suppress partial publish; emit combined at end
		}
		// MessageDeltaEvent carries output tokens alongside Completed.
		if uEv.Completed != nil {
			if uEv.Usage != nil {
				outputTokens = uEv.Usage.Tokens
				uEv.Usage = nil
			}
			stopReason = uEv.Completed.StopReason
			uEv.Completed = nil // suppress early completed; emit at end
		}

		// Publish everything except the suppressed usage/completed fields.
		if uEv.Started != nil || uEv.Delta != nil || uEv.ToolCall != nil || uEv.Content != nil || uEv.Error != nil {
			if err := Publish(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}

	// Emit combined usage record and completed after stream ends.
	allTokens := append(inputTokens, outputTokens...).NonZero() //nolint:gocritic

	if len(allTokens) > 0 || stopReason != "" {
		rec := usage.Record{
			Dims: usage.Dims{
				Provider:  provider,
				Model:     sctx.Model,
				RequestID: requestID,
			},
			Tokens:     allTokens,
			RecordedAt: time.Now(),
		}
		if sctx.CostCalc != nil {
			if cost, ok := sctx.CostCalc.Calculate(provider, sctx.Model, allTokens); ok {
				rec.Cost = cost
			}
		}
		if sctx.RateLimits != nil {
			rec.Extras = map[string]any{"rate_limits": sctx.RateLimits}
		}
		pub.UsageRecord(rec)
	}

	pub.Completed(llm.CompletedEvent{StopReason: stopReason})
}

// StreamCompletions consumes a completions.StreamHandle and publishes canonical
// events to pub. Unlike the Messages protocol, completions events are largely
// self-contained so token accumulation is simpler.
func StreamCompletions(
	ctx context.Context,
	handle *apicore.StreamHandle,
	pub llm.Publisher,
	sctx StreamContext,
) {
	_ = ctx

	var (
		requestID   string
		allTokens   usage.TokenItems
		stopReason  llm.StopReason
		startedOnce bool
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

		uEv, ignored, err := EventFromCompletions(result.Event)
		if err != nil {
			pub.Error(err)
			return
		}
		if ignored {
			continue
		}

		// Completions: Started is co-emitted with the first delta chunk.
		if uEv.Started != nil && !startedOnce {
			startedOnce = true
			requestID = uEv.Started.RequestID
			if uEv.Started.Model != "" && uEv.Started.Model != sctx.Model {
				pub.ModelResolved(provider, sctx.Model, uEv.Started.Model)
			}
			uEv.Started.Provider = upstreamProvider
			if sctx.RateLimits != nil {
				uEv.Started.Extra = map[string]any{"rate_limits": sctx.RateLimits}
			}
		} else if uEv.Started != nil {
			uEv.Started = nil // don't re-emit Started on subsequent chunks
		}

		// Accumulate usage and stop reason; emit combined at end.
		if uEv.Usage != nil {
			allTokens = append(allTokens, uEv.Usage.Tokens...)
			uEv.Usage = nil
		}
		if uEv.Completed != nil {
			stopReason = uEv.Completed.StopReason
			uEv.Completed = nil
		}

		if uEv.Started != nil || uEv.Delta != nil || uEv.ToolCall != nil || uEv.Content != nil || uEv.Error != nil {
			if err := Publish(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}

	allTokens = allTokens.NonZero()
	if len(allTokens) > 0 || stopReason != "" {
		rec := usage.Record{
			Dims: usage.Dims{
				Provider:  provider,
				Model:     sctx.Model,
				RequestID: requestID,
			},
			Tokens:     allTokens,
			RecordedAt: time.Now(),
		}
		if sctx.CostCalc != nil {
			if cost, ok := sctx.CostCalc.Calculate(provider, sctx.Model, allTokens); ok {
				rec.Cost = cost
			}
		}
		if sctx.RateLimits != nil {
			rec.Extras = map[string]any{"rate_limits": sctx.RateLimits}
		}
		pub.UsageRecord(rec)
	}
	pub.Completed(llm.CompletedEvent{StopReason: stopReason})
}

// StreamResponses consumes a responses.StreamHandle and publishes canonical
// events to pub.
func StreamResponses(
	ctx context.Context,
	handle *apicore.StreamHandle,
	pub llm.Publisher,
	sctx StreamContext,
) {
	_ = ctx

	var (
		requestID  string
		allTokens  usage.TokenItems
		stopReason llm.StopReason
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

		uEv, ignored, err := EventFromResponses(result.Event)
		if err != nil {
			pub.Error(err)
			return
		}
		if ignored {
			continue
		}

		if uEv.Started != nil {
			requestID = uEv.Started.RequestID
			if uEv.Started.Model != "" && uEv.Started.Model != sctx.Model {
				pub.ModelResolved(provider, sctx.Model, uEv.Started.Model)
			}
			uEv.Started.Provider = upstreamProvider
			if sctx.RateLimits != nil {
				uEv.Started.Extra = map[string]any{"rate_limits": sctx.RateLimits}
			}
		}

		if uEv.Usage != nil {
			allTokens = append(allTokens, uEv.Usage.Tokens...)
			uEv.Usage = nil
		}
		if uEv.Completed != nil {
			stopReason = uEv.Completed.StopReason
			uEv.Completed = nil
		}

		if uEv.Started != nil || uEv.Delta != nil || uEv.ToolCall != nil || uEv.Content != nil || uEv.Error != nil {
			if err := Publish(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}

	allTokens = allTokens.NonZero()
	if len(allTokens) > 0 || stopReason != "" {
		rec := usage.Record{
			Dims: usage.Dims{
				Provider:  provider,
				Model:     sctx.Model,
				RequestID: requestID,
			},
			Tokens:     allTokens,
			RecordedAt: time.Now(),
		}
		if sctx.CostCalc != nil {
			if cost, ok := sctx.CostCalc.Calculate(provider, sctx.Model, allTokens); ok {
				rec.Cost = cost
			}
		}
		if sctx.RateLimits != nil {
			rec.Extras = map[string]any{"rate_limits": sctx.RateLimits}
		}
		pub.UsageRecord(rec)
	}
	pub.Completed(llm.CompletedEvent{StopReason: stopReason})
}

