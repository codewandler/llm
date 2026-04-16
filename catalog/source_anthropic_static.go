package catalog

import (
	"context"
	"time"
)

const anthropicSourceID = "anthropic-static"

type AnthropicStaticSource struct{}

func NewAnthropicStaticSource() AnthropicStaticSource { return AnthropicStaticSource{} }

func (AnthropicStaticSource) ID() string { return anthropicSourceID }

func (AnthropicStaticSource) Fetch(context.Context) (*Fragment, error) {
	observedAt := time.Now().UTC()
	service := Service{
		ID:       "anthropic",
		Name:     "Anthropic",
		Kind:     ServiceKindDirect,
		Operator: "anthropic",
		Provenance: []Provenance{{
			SourceID:   anthropicSourceID,
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
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"},
			name:    "Claude Sonnet 4.6",
			aliases: []string{"claude-sonnet-4-6", "sonnet"},
			capabilities: Capabilities{
				Reasoning:           true,
				ToolUse:             true,
				Streaming:           true,
				Caching:             true,
				InterleavedThinking: true,
				AdaptiveThinking:    true,
				Temperature:         true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75},
			offerings: []staticOffering{{wireID: "claude-sonnet-4-6", aliases: []string{"default", "fast", "sonnet"}}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"},
			name:    "Claude Opus 4.6",
			aliases: []string{"claude-opus-4-6", "opus"},
			capabilities: Capabilities{
				Reasoning:           true,
				ToolUse:             true,
				Streaming:           true,
				Caching:             true,
				InterleavedThinking: true,
				AdaptiveThinking:    true,
				Temperature:         true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 5.0, Output: 25.0, CachedInput: 0.50, CacheWrite: 6.25},
			offerings: []staticOffering{{wireID: "claude-opus-4-6", aliases: []string{"powerful", "opus"}}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "haiku", Version: "4.5", ReleaseDate: "2025-10-01"},
			name:    "Claude Haiku 4.5",
			aliases: []string{"claude-haiku-4-5", "claude-haiku-4-5-20251001", "haiku"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Caching:     true,
				Temperature: true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 1.0, Output: 5.0, CachedInput: 0.10, CacheWrite: 1.25},
			offerings: []staticOffering{{wireID: "claude-haiku-4-5-20251001", aliases: []string{"haiku"}}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5", ReleaseDate: "2025-09-29"},
			name:    "Claude Sonnet 4.5",
			aliases: []string{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Caching:     true,
				Temperature: true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75},
			offerings: []staticOffering{{wireID: "claude-sonnet-4-5-20250929"}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.5", ReleaseDate: "2025-11-01"},
			name:    "Claude Opus 4.5",
			aliases: []string{"claude-opus-4-5", "claude-opus-4-5-20251101"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Caching:     true,
				Temperature: true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 5.0, Output: 25.0, CachedInput: 0.50, CacheWrite: 6.25},
			offerings: []staticOffering{{wireID: "claude-opus-4-5"}, {wireID: "claude-opus-4-5-20251101"}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.1", ReleaseDate: "2025-08-05"},
			name:    "Claude Opus 4.1",
			aliases: []string{"claude-opus-4-1", "claude-opus-4-1-20250805"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Caching:     true,
				Temperature: true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75},
			offerings: []staticOffering{{wireID: "claude-opus-4-1"}, {wireID: "claude-opus-4-1-20250805"}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.0", ReleaseDate: "2025-05-14"},
			name:    "Claude Opus 4.0",
			aliases: []string{"claude-opus-4", "claude-opus-4-20250514"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Caching:     true,
				Temperature: true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 15.0, Output: 75.0, CachedInput: 1.50, CacheWrite: 18.75},
			offerings: []staticOffering{{wireID: "claude-opus-4"}, {wireID: "claude-opus-4-20250514"}},
		},
		{
			key:     ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.0", ReleaseDate: "2025-05-14"},
			name:    "Claude Sonnet 4.0",
			aliases: []string{"claude-sonnet-4", "claude-sonnet-4-20250514"},
			capabilities: Capabilities{
				Reasoning:   true,
				ToolUse:     true,
				Streaming:   true,
				Caching:     true,
				Temperature: true,
			},
			limits:    Limits{ContextWindow: 200000, MaxOutput: 32000},
			pricing:   &Pricing{Input: 3.0, Output: 15.0, CachedInput: 0.30, CacheWrite: 3.75},
			offerings: []staticOffering{{wireID: "claude-sonnet-4"}, {wireID: "claude-sonnet-4-20250514"}},
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
			InputModalities:  []string{"text", "image"},
			OutputModalities: []string{"text"},
			ReferencePricing: model.pricing,
			Provenance: []Provenance{{
				SourceID:   anthropicSourceID,
				Authority:  string(AuthorityCanonical),
				ObservedAt: observedAt,
				RawID:      model.name,
			}},
		}
		fragment.Models = append(fragment.Models, modelRecord)
		for _, offering := range model.offerings {
			fragment.Offerings = append(fragment.Offerings, Offering{
				ServiceID:   service.ID,
				WireModelID: offering.wireID,
				ModelKey:    modelRecord.Key,
				Aliases:     offering.aliases,
				APITypes:    []string{"anthropic-messages"},
				Pricing:     model.pricing,
				Provenance: []Provenance{{
					SourceID:   anthropicSourceID,
					Authority:  string(AuthorityCanonical),
					ObservedAt: observedAt,
					RawID:      offering.wireID,
				}},
			})
		}
	}
	return fragment, nil
}
