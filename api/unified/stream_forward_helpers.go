package unified

import (
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/usage"
)

// StreamContext carries provider metadata used to enrich unified events before
// forwarding them into the current llm publisher path.
type StreamContext struct {
	Provider         string
	Model            string
	UpstreamProvider string
	CostCalc         usage.CostCalculator
	CostProvider     string
	CostModel        string
	RateLimits       *llm.RateLimits
	UsageExtras      map[string]any
}

func (s StreamContext) costLookupProvider() string {
	if s.CostProvider != "" {
		return s.CostProvider
	}
	if s.UpstreamProvider != "" {
		return s.UpstreamProvider
	}
	return s.Provider
}

func (s StreamContext) costLookupModel(resolved string) string {
	if s.CostModel != "" {
		return s.CostModel
	}
	if resolved != "" {
		return resolved
	}
	return s.Model
}

func mergeExtras(maps ...map[string]any) map[string]any {
	var out map[string]any
	for _, src := range maps {
		if len(src) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(src))
		}
		for k, v := range src {
			out[k] = v
		}
	}
	return out
}

func hasUnifiedPayload(ev StreamEvent) bool {
	return ev.Started != nil || ev.Delta != nil || ev.ToolCall != nil || ev.Content != nil || ev.Error != nil || ev.Lifecycle != nil || ev.ContentDelta != nil || ev.StreamContent != nil || ev.ToolDelta != nil || ev.StreamToolCall != nil || ev.Annotation != nil || ev.Type == StreamEventUnknown || ev.Extras.RawEventName != ""
}

func emitUsageRecord(pub llm.Publisher, sctx StreamContext, provider, requestID, responseModel string, tokens usage.TokenItems) {
	if len(tokens) == 0 {
		return
	}
	rec := usage.Record{
		Dims:       usage.Dims{Provider: provider, Model: sctx.Model, RequestID: requestID},
		Tokens:     tokens,
		RecordedAt: time.Now(),
	}
	if sctx.CostCalc != nil {
		costProvider := sctx.costLookupProvider()
		costModel := sctx.costLookupModel(responseModel)
		if cost, ok := sctx.CostCalc.Calculate(costProvider, costModel, tokens); ok {
			rec.Cost = cost
		}
	}
	rec.Extras = mergeExtras(sctx.UsageExtras)
	if sctx.RateLimits != nil {
		rec.Extras = mergeExtras(rec.Extras, map[string]any{"rate_limits": sctx.RateLimits})
	}
	pub.UsageRecord(rec)
}
