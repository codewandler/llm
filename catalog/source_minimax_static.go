package catalog

import (
	"context"
	"strings"
	"time"
)

const minimaxSourceID = "minimax-static"

type MiniMaxStaticSource struct{}

func NewMiniMaxStaticSource() MiniMaxStaticSource { return MiniMaxStaticSource{} }

func (MiniMaxStaticSource) ID() string { return minimaxSourceID }

func (MiniMaxStaticSource) Fetch(context.Context) (*Fragment, error) {
	observedAt := time.Now().UTC()

	service := Service{
		ID:       "minimax",
		Name:     "MiniMax",
		Kind:     ServiceKindDirect,
		Operator: "minimax",
		Provenance: []Provenance{{
			SourceID:   minimaxSourceID,
			Authority:  string(AuthorityCanonical),
			ObservedAt: observedAt,
		}},
	}

	type staticOffering struct {
		wireID  string
		aliases []string
	}

	type staticModel struct {
		key          ModelKey
		name         string
		aliases      []string
		capabilities Capabilities
		limits       Limits
		pricing      *Pricing
		offerings    []staticOffering
	}

	models := []staticModel{
		{
			key:     ModelKey{Creator: "minimax", Family: "m2", Series: "standard", Version: "2.7"},
			name:    "MiniMax M2.7",
			aliases: []string{"minimax-m2-7"},
			capabilities: Capabilities{
				Reasoning:           true,
				ToolUse:             true,
				Streaming:           true,
				InterleavedThinking: true,
				AdaptiveThinking:    true,
				Temperature:         true,
			},
			limits:  Limits{ContextWindow: 1000000, MaxOutput: 32000},
			pricing: &Pricing{Input: 2.1, Output: 8.4, CachedInput: 0.42, CacheWrite: 2.625},
			offerings: []staticOffering{
				{wireID: "MiniMax-M2.7", aliases: []string{"minimax"}},
				{wireID: "MiniMax-M2.7-highspeed", aliases: []string{"highspeed"}},
			},
		},
		{
			key:     ModelKey{Creator: "minimax", Family: "m2", Series: "standard", Version: "2.5"},
			name:    "MiniMax M2.5",
			aliases: []string{"minimax-m2-5"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Temperature: true,
			},
			limits:  Limits{ContextWindow: 1000000, MaxOutput: 32000},
			pricing: &Pricing{Input: 2.1, Output: 8.4, CachedInput: 0.21, CacheWrite: 2.625},
			offerings: []staticOffering{
				{wireID: "MiniMax-M2.5", aliases: []string{}},
				{wireID: "MiniMax-M2.5-highspeed", aliases: []string{"highspeed"}},
			},
		},
		{
			key:     ModelKey{Creator: "minimax", Family: "m2", Series: "standard", Version: "2.1"},
			name:    "MiniMax M2.1",
			aliases: []string{"minimax-m2-1"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Temperature: true,
			},
			limits:  Limits{ContextWindow: 1000000, MaxOutput: 32000},
			pricing: &Pricing{Input: 2.1, Output: 8.4, CachedInput: 0.21, CacheWrite: 2.625},
			offerings: []staticOffering{
				{wireID: "MiniMax-M2.1", aliases: []string{}},
				{wireID: "MiniMax-M2.1-highspeed", aliases: []string{"highspeed"}},
			},
		},
		{
			key:     ModelKey{Creator: "minimax", Family: "m2", Series: "standard", Version: "2"},
			name:    "MiniMax M2",
			aliases: []string{"minimax-m2"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Temperature: true,
			},
			limits:  Limits{ContextWindow: 1000000, MaxOutput: 32000},
			pricing: &Pricing{Input: 2.1, Output: 8.4, CachedInput: 0.21, CacheWrite: 2.625},
			offerings: []staticOffering{
				{wireID: "MiniMax-M2", aliases: []string{}},
			},
		},
	}

	fragment := &Fragment{Services: []Service{service}}
	for _, model := range models {
		modelRecord := ModelRecord{
			Key:              NormalizeKey(model.key),
			Name:             model.name,
			Aliases:          model.aliases,
			Canonical:        true,
			Capabilities:     model.capabilities,
			Limits:           model.limits,
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
			ReferencePricing: model.pricing,
			Provenance: []Provenance{{
				SourceID:   minimaxSourceID,
				Authority:  string(AuthorityCanonical),
				ObservedAt: observedAt,
				RawID:      model.name,
			}},
		}
		fragment.Models = append(fragment.Models, modelRecord)

		highspeedPricing := &Pricing{Input: 4.2, Output: 16.8, CachedInput: 0.42, CacheWrite: 2.625}

		highspeedKey := NormalizeKey(ModelKey{
			Creator: model.key.Creator,
			Family:  model.key.Family,
			Series:  model.key.Series,
			Version: model.key.Version,
			Variant: "highspeed",
		})
		highspeedRecord := ModelRecord{
			Key:              highspeedKey,
			Name:             model.name + " Highspeed",
			Aliases:          []string{},
			Canonical:        true,
			Capabilities:     model.capabilities,
			Limits:           model.limits,
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
			ReferencePricing: highspeedPricing,
			Provenance: []Provenance{{
				SourceID:   minimaxSourceID,
				Authority:  string(AuthorityCanonical),
				ObservedAt: observedAt,
				RawID:      model.name + " Highspeed",
			}},
		}

		for _, offering := range model.offerings {
			isHighspeed := strings.Contains(offering.wireID, "highspeed")
			var offeringKey ModelKey
			var offeringPricing *Pricing
			if isHighspeed {
				offeringKey = highspeedKey
				offeringPricing = highspeedPricing
			} else {
				offeringKey = modelRecord.Key
				offeringPricing = model.pricing
			}
			fragment.Offerings = append(fragment.Offerings, Offering{
				ServiceID:   service.ID,
				WireModelID: offering.wireID,
				ModelKey:    offeringKey,
				Aliases:     offering.aliases,
				APITypes:    []string{"anthropic-messages"},
				Pricing:     offeringPricing,
				Provenance: []Provenance{{
					SourceID:   minimaxSourceID,
					Authority:  string(AuthorityCanonical),
					ObservedAt: observedAt,
					RawID:      offering.wireID,
				}},
			})
			if isHighspeed {
				fragment.Models = append(fragment.Models, highspeedRecord)
			}
		}
	}
	return fragment, nil
}
