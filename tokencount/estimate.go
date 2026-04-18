package tokencount

import (
	"context"
	"time"

	"github.com/codewandler/llm"
	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	"github.com/codewandler/llm/usage"
)

type EstimateResult struct {
	Tokens    usage.TokenItems
	Cost      usage.Cost
	CostKnown bool
	Encoder   string
}

func Estimate(ctx context.Context, provider string, req llm.Request) *EstimateResult {
	if req.Model == "" {
		return nil
	}

	profile := profileForProvider(provider, req.Model)

	tc := &TokenCount{}
	var err error
	if profile.AnthropicTools && len(req.Tools) > 0 {
		err = CountMessagesAndToolsAnthropic(tc, TokenCountRequest{
			Model:    req.Model,
			Messages: req.Messages,
			Tools:    req.Tools,
		})
	} else {
		err = CountMessagesAndTools(tc, TokenCountRequest{
			Model:    req.Model,
			Messages: req.Messages,
			Tools:    req.Tools,
		}, profile.CountOpts())
	}
	if err != nil || tc.InputTokens == 0 {
		return nil
	}

	tokens := usage.TokenItems{{Kind: usage.KindInput, Count: tc.InputTokens}}
	var cost usage.Cost
	var costKnown bool
	if c, ok := usage.Default().Calculate(provider, req.Model, tokens); ok {
		c.Source = "estimated"
		cost = c
		costKnown = true
	}

	return &EstimateResult{
		Tokens:    tokens,
		Cost:      cost,
		CostKnown: costKnown,
		Encoder:   tc.Encoder,
	}
}

type tokenProfile struct {
	Encoding       string
	PerMsgOverhead int
	ReplyPriming   int
	AnthropicTools bool
}

func (p tokenProfile) CountOpts() CountOpts {
	return CountOpts{
		Encoding:       p.Encoding,
		PerMsgOverhead: p.PerMsgOverhead,
		ReplyPriming:   p.ReplyPriming,
	}
}

func profileForProvider(provider, model string) tokenProfile {
	identity, ok := modelcatalog.ResolveWireModelIdentity(provider, model)
	if !ok {
		return profileFromModelID(model)
	}

	creator := identity.Creator
	family := identity.Family

	switch {
	case creator == "anthropic" && family == "claude":
		return tokenProfile{Encoding: EncodingCL100K, AnthropicTools: true}
	case creator == "openai":
		enc, _ := EncodingForModel(model)
		return tokenProfile{Encoding: enc, PerMsgOverhead: 4, ReplyPriming: 3}
	case creator == "minimax":
		return tokenProfile{Encoding: EncodingMinimax, PerMsgOverhead: 3}
	default:
		return profileFromModelID(model)
	}
}

func profileFromModelID(model string) tokenProfile {
	enc, _ := EncodingForModel(model)
	switch enc {
	case EncodingO200K:
		return tokenProfile{Encoding: enc, PerMsgOverhead: 4, ReplyPriming: 3}
	default:
		return tokenProfile{Encoding: enc}
	}
}

// EstimateRecords converts a TokenCount into a primary usage.Record (unlabeled,
// containing the total input count) followed by per-segment labeled breakdown
// records. The primary is always first so that consumers can rely on [0] being
// the summary when iterating.
func EstimateRecords(
	est *TokenCount,
	provider, model string,
	source string,
	calculator usage.CostCalculator,
) []usage.Record {
	if est == nil {
		return nil
	}

	now := time.Now()
	dims := usage.Dims{Provider: provider, Model: model}

	primaryTokens := usage.TokenItems{{Kind: usage.KindInput, Count: est.InputTokens}}
	primary := usage.Record{
		IsEstimate: true,
		Source:     source,
		Encoder:    est.Encoder,
		RecordedAt: now,
		Dims:       dims,
		Tokens:     primaryTokens,
	}
	if calculator != nil {
		if cost, ok := calculator.Calculate(provider, model, primaryTokens); ok {
			cost.Source = "estimated"
			primary.Cost = cost
		}
	}

	records := []usage.Record{primary}

	type segment struct {
		label string
		count int
	}
	segments := []segment{
		{"system", est.SystemTokens},
		{"user", est.UserTokens},
		{"assistant", est.AssistantTokens},
		{"tool_results", est.ToolResultTokens},
		{"tools", est.ToolsTokens},
		{"overhead", est.OverheadTokens},
	}
	for _, seg := range segments {
		if seg.count <= 0 {
			continue
		}
		rec := usage.Record{
			IsEstimate: true,
			Source:     source,
			RecordedAt: now,
			Dims: usage.Dims{
				Provider: provider,
				Model:    model,
				Labels:   map[string]string{"category": seg.label},
			},
			Tokens: usage.TokenItems{{Kind: usage.KindInput, Count: seg.count}},
		}
		records = append(records, rec)
	}

	return records
}
