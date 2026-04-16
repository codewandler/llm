package codex

import (
	"context"
	"sort"
	"time"

	"github.com/codewandler/llm/catalog"
)

const (
	runtimeSourceID = "codex-runtime"
	serviceID       = "codex"
	runtimeID       = "codex-local"
)

type RuntimeSource struct{}

func NewRuntimeSource() RuntimeSource { return RuntimeSource{} }

func (RuntimeSource) ID() string { return runtimeSourceID }

func (RuntimeSource) Fetch(context.Context) (*catalog.Fragment, error) {
	observedAt := time.Now().UTC()
	provenance := func(rawID string) []catalog.Provenance {
		return []catalog.Provenance{{
			SourceID:   runtimeSourceID,
			Authority:  string(catalog.AuthorityLocal),
			ObservedAt: observedAt,
			RawID:      rawID,
		}}
	}

	fragment := &catalog.Fragment{
		Services: []catalog.Service{{
			ID:       serviceID,
			Name:     "OpenAI Codex",
			Kind:     catalog.ServiceKindPlatform,
			Operator: "openai",
			APIURL:   defaultBaseURL + "/codex",
			Package:  "github.com/codewandler/llm/provider/codex",
			Provenance: []catalog.Provenance{{
				SourceID:   runtimeSourceID,
				Authority:  string(catalog.AuthorityLocal),
				ObservedAt: observedAt,
			}},
		}},
		Runtimes: []catalog.Runtime{{
			ID:        runtimeID,
			ServiceID: serviceID,
			Name:      "Local Codex Auth",
			Endpoint:  defaultBaseURL + "/codex",
			Provenance: []catalog.Provenance{{
				SourceID:   runtimeSourceID,
				Authority:  string(catalog.AuthorityLocal),
				ObservedAt: observedAt,
			}},
		}},
	}

	models := EmbeddedModels()
	aliasMap := ModelAliases()
	aliasesByTarget := make(map[string][]string)
	for alias, target := range aliasMap {
		aliasesByTarget[target] = append(aliasesByTarget[target], alias)
	}

	type offeringEntry struct {
		ref      catalog.OfferingRef
		offering catalog.Offering
		access   catalog.RuntimeAccess
		acquire  catalog.RuntimeAcquisition
	}

	modelRecords := make(map[catalog.ModelKey]catalog.ModelRecord)
	offeringRecords := make(map[catalog.OfferingRef]offeringEntry)
	for _, model := range models {
		key, ok := modelKeyForSlug(model.Slug)
		if !ok {
			continue
		}
		if _, exists := modelRecords[key]; !exists {
			modelRecords[key] = catalog.ModelRecord{
				Key:       key,
				Name:      firstNonEmpty(model.DisplayName, model.Slug),
				Canonical: false,
				Capabilities: catalog.Capabilities{
					Reasoning:        model.DefaultReasoningLevel != nil && *model.DefaultReasoningLevel != "",
					ToolUse:          true,
					StructuredOutput: true,
					Streaming:        true,
				},
				Provenance: provenance(model.Slug),
			}
		}
		ref := catalog.OfferingRef{ServiceID: serviceID, WireModelID: model.Slug}
		offeringRecords[ref] = offeringEntry{
			ref: ref,
			offering: catalog.Offering{
				ServiceID:   serviceID,
				WireModelID: model.Slug,
				ModelKey:    key,
				Aliases:     aliasesByTarget[model.Slug],
				APITypes:    []string{"openai-responses"},
				Provenance:  provenance(model.Slug),
			},
			access: catalog.RuntimeAccess{
				RuntimeID:      runtimeID,
				Offering:       ref,
				Routable:       true,
				ResolvedWireID: model.Slug,
				Provenance:     provenance(model.Slug),
			},
			acquire: catalog.RuntimeAcquisition{
				RuntimeID:  runtimeID,
				Offering:   ref,
				Known:      true,
				Acquirable: false,
				Status:     "available",
				Action:     "none",
				Provenance: provenance(model.Slug),
			},
		}
	}

	keys := make([]catalog.ModelKey, 0, len(modelRecords))
	for key := range modelRecords {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return catalog.LineID(keys[i]) < catalog.LineID(keys[j])
	})
	for _, key := range keys {
		fragment.Models = append(fragment.Models, modelRecords[key])
	}

	refs := make([]catalog.OfferingRef, 0, len(offeringRecords))
	for ref := range offeringRecords {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ServiceID != refs[j].ServiceID {
			return refs[i].ServiceID < refs[j].ServiceID
		}
		return refs[i].WireModelID < refs[j].WireModelID
	})
	for _, ref := range refs {
		entry := offeringRecords[ref]
		fragment.Offerings = append(fragment.Offerings, entry.offering)
		fragment.RuntimeAccess = append(fragment.RuntimeAccess, entry.access)
		fragment.RuntimeAcquisition = append(fragment.RuntimeAcquisition, entry.acquire)
	}

	return fragment, nil
}
