package catalog

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/codewandler/llm/catalog/modeldb"
)

const modelDBSourceID = "modeldb"

type ModelDBSource struct{}

func NewModelDBSource() ModelDBSource { return ModelDBSource{} }

func (ModelDBSource) ID() string { return modelDBSourceID }

func (ModelDBSource) Fetch(context.Context) (*Fragment, error) {
	db, err := modeldb.Load()
	if err != nil {
		return nil, err
	}
	observedAt := time.Now().UTC()
	fragment := &Fragment{}

	providerIDs := make([]string, 0, len(db))
	for providerID := range db {
		providerIDs = append(providerIDs, providerID)
	}
	sort.Strings(providerIDs)

	for _, providerID := range providerIDs {
		provider := db[providerID]
		serviceID, ok := serviceIDForModelDBProvider(providerID)
		if !ok {
			continue
		}
		fragment.Services = append(fragment.Services, Service{
			ID:       serviceID,
			Name:     provider.Name,
			Kind:     serviceKindForSourceService(serviceID),
			Operator: serviceOperatorForSourceService(serviceID),
			Provenance: []Provenance{{
				SourceID:   modelDBSourceID,
				Authority:  string(AuthorityEnrichment),
				ObservedAt: observedAt,
				RawID:      providerID,
			}},
		})

		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)

		for _, modelID := range modelIDs {
			entry := provider.Models[modelID]
			key, ok := inferModelKey(serviceID, modelID)
			if !ok {
				continue
			}
			if key.ReleaseDate == "" && entry.ReleaseDate != "" {
				key.ReleaseDate = normalizeDate(entry.ReleaseDate)
				key = NormalizeKey(key)
			}
			modelRecord := ModelRecord{
				Key:       key,
				Canonical: false,
				Provenance: []Provenance{{
					SourceID:   modelDBSourceID,
					Authority:  string(AuthorityEnrichment),
					ObservedAt: observedAt,
					RawID:      modelID,
				}},
			}
			if serviceID == "anthropic" || serviceID == "openai" {
				modelRecord.Capabilities = capabilitiesFromModelDB(entry)
				modelRecord.InputModalities = normalizeStrings(entry.Modalities.Input)
				modelRecord.OutputModalities = normalizeStrings(entry.Modalities.Output)
			}
			fragment.Models = append(fragment.Models, modelRecord)
			if serviceID == "bedrock" {
				fragment.Offerings = append(fragment.Offerings, Offering{
					ServiceID:   serviceID,
					WireModelID: modelID,
					ModelKey:    key,
					Pricing: &Pricing{
						Input:       entry.Cost.Input,
						Output:      entry.Cost.Output,
						CachedInput: entry.Cost.CacheRead,
						CacheWrite:  entry.Cost.CacheWrite,
					},
					LimitsOverride: limitsPtr(entry.Limit.Context, entry.Limit.Output),
					Provenance: []Provenance{{
						SourceID:   modelDBSourceID,
						Authority:  string(AuthorityEnrichment),
						ObservedAt: observedAt,
						RawID:      modelID,
					}},
				})
			}
		}
	}
	return fragment, nil
}

func capabilitiesFromModelDB(entry modeldb.Model) Capabilities {
	return Capabilities{
		Reasoning:           entry.Reasoning,
		ToolUse:             entry.ToolCall,
		StructuredOutput:    entry.StructuredOutput,
		Vision:              containsString(entry.Modalities.Input, "image"),
		InterleavedThinking: entry.Interleaved != nil && entry.Interleaved.Enabled,
		Temperature:         entry.Temperature,
	}
}

func serviceIDForModelDBProvider(providerID string) (string, bool) {
	switch providerID {
	case "anthropic":
		return "anthropic", true
	case "openai":
		return "openai", true
	case "openrouter":
		return "openrouter", true
	case "amazon-bedrock":
		return "bedrock", true
	default:
		return "", false
	}
}

func serviceKindForSourceService(serviceID string) ServiceKind {
	switch serviceID {
	case "openrouter":
		return ServiceKindBroker
	case "bedrock":
		return ServiceKindPlatform
	default:
		return ServiceKindDirect
	}
}

func serviceOperatorForSourceService(serviceID string) string {
	switch serviceID {
	case "openrouter":
		return "openrouter"
	case "bedrock":
		return "aws"
	default:
		return serviceID
	}
}

func limitsPtr(contextWindow, maxOutput int) *Limits {
	if contextWindow == 0 && maxOutput == 0 {
		return nil
	}
	return &Limits{ContextWindow: contextWindow, MaxOutput: maxOutput}
}

func containsString(values []string, want string) bool {
	want = normalizeKeyPart(want)
	for _, v := range values {
		if normalizeKeyPart(strings.TrimSpace(v)) == want {
			return true
		}
	}
	return false
}
