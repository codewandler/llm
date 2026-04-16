package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

const defaultOllamaBaseURL = "http://localhost:11434"

type KnownRuntimeModel struct {
	ID   string
	Name string
}

type OllamaRuntimeSource struct {
	BaseURL     string
	Client      *http.Client
	RuntimeID   string
	RuntimeName string
	KnownModels []KnownRuntimeModel
}

func NewOllamaRuntimeSource() OllamaRuntimeSource {
	return OllamaRuntimeSource{}
}

func (OllamaRuntimeSource) ID() string { return "ollama-runtime" }

func (s OllamaRuntimeSource) Fetch(ctx context.Context) (*Fragment, error) {
	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	observedAt := time.Now().UTC()
	runtimeID := firstNonEmpty(s.RuntimeID, "ollama-local")
	runtimeName := firstNonEmpty(s.RuntimeName, "Ollama Local")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama runtime source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama runtime source: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	known := s.KnownModels
	if known == nil {
		known = defaultOllamaKnownModels()
	}
	installed := make(map[string]string, len(payload.Models))
	for _, model := range payload.Models {
		installed[model.Name] = model.Name
	}

	models := make(map[ModelKey]ModelRecord)
	offerings := make(map[OfferingRef]Offering)
	access := make(map[OfferingRef]RuntimeAccess)
	acquisition := make(map[OfferingRef]RuntimeAcquisition)

	service := Service{
		ID:       "ollama",
		Name:     "Ollama",
		Kind:     ServiceKindLocal,
		Operator: "ollama",
		Provenance: []Provenance{{
			SourceID:   s.ID(),
			Authority:  string(AuthorityLocal),
			ObservedAt: observedAt,
		}},
	}
	runtime := Runtime{
		ID:        runtimeID,
		ServiceID: service.ID,
		Name:      runtimeName,
		Local:     true,
		Endpoint:  baseURL,
		Provenance: []Provenance{{
			SourceID:   s.ID(),
			Authority:  string(AuthorityLocal),
			ObservedAt: observedAt,
		}},
	}

	for _, model := range known {
		ref, record, offering := localRuntimeEntries(service.ID, model.ID, model.Name, s.ID(), observedAt)
		models[record.Key] = record
		offerings[ref] = offering
		status := RuntimeAcquisition{
			RuntimeID:  runtimeID,
			Offering:   ref,
			Known:      true,
			Acquirable: true,
			Status:     "pullable",
			Action:     "pull",
			Provenance: []Provenance{{SourceID: s.ID(), Authority: string(AuthorityLocal), ObservedAt: observedAt, RawID: model.ID}},
		}
		acquisition[ref] = status
	}

	for modelID := range installed {
		ref, record, offering := localRuntimeEntries(service.ID, modelID, modelID, s.ID(), observedAt)
		models[record.Key] = record
		offerings[ref] = offering
		access[ref] = RuntimeAccess{
			RuntimeID:      runtimeID,
			Offering:       ref,
			Routable:       true,
			ResolvedWireID: modelID,
			Provenance:     []Provenance{{SourceID: s.ID(), Authority: string(AuthorityLocal), ObservedAt: observedAt, RawID: modelID}},
		}
		acquisition[ref] = RuntimeAcquisition{
			RuntimeID:  runtimeID,
			Offering:   ref,
			Known:      true,
			Acquirable: false,
			Status:     "installed",
			Action:     "none",
			Provenance: []Provenance{{SourceID: s.ID(), Authority: string(AuthorityLocal), ObservedAt: observedAt, RawID: modelID}},
		}
	}

	fragment := &Fragment{Services: []Service{service}, Runtimes: []Runtime{runtime}}
	for _, key := range sortedModelKeys(models) {
		fragment.Models = append(fragment.Models, models[key])
	}
	for _, ref := range sortedOfferingRefs(offerings) {
		fragment.Offerings = append(fragment.Offerings, offerings[ref])
		if item, ok := access[ref]; ok {
			fragment.RuntimeAccess = append(fragment.RuntimeAccess, item)
		}
		if item, ok := acquisition[ref]; ok {
			fragment.RuntimeAcquisition = append(fragment.RuntimeAcquisition, item)
		}
	}
	return fragment, nil
}

func defaultOllamaKnownModels() []KnownRuntimeModel {
	return []KnownRuntimeModel{
		{ID: "glm-4.7-flash", Name: "GLM-4.7 Flash"},
		{ID: "ministral-3:8b", Name: "Ministral 3 8B"},
		{ID: "rnj-1", Name: "RNJ-1"},
		{ID: "functiongemma", Name: "FunctionGemma"},
		{ID: "devstral-small-2", Name: "Devstral Small 2"},
		{ID: "nemotron-3-nano:30b", Name: "Nemotron 3 Nano 30B"},
		{ID: "llama3.2:1b", Name: "Llama 3.2 1B"},
		{ID: "qwen3:1.7b", Name: "Qwen 3 1.7B"},
		{ID: "qwen3:0.6b", Name: "Qwen 3 0.6B"},
		{ID: "granite3.1-moe:1b", Name: "Granite 3.1 MoE 1B"},
		{ID: "qwen2.5:0.5b", Name: "Qwen 2.5 0.5B"},
	}
}

func localRuntimeEntries(serviceID, modelID, fallbackName, sourceID string, observedAt time.Time) (OfferingRef, ModelRecord, Offering) {
	key, ok := inferModelKey(serviceID, modelID)
	if !ok {
		key = provisionalLocalKey(serviceID, modelID)
	}
	name := fallbackName
	if name == "" {
		name = inferModelNameFromKey(key)
	}
	ref := OfferingRef{ServiceID: serviceID, WireModelID: modelID}
	provenance := []Provenance{{SourceID: sourceID, Authority: string(AuthorityLocal), ObservedAt: observedAt, RawID: modelID}}
	return ref,
		ModelRecord{Key: key, Name: name, Canonical: false, Provenance: provenance},
		Offering{ServiceID: serviceID, WireModelID: modelID, ModelKey: key, Provenance: provenance}
}

func sortedOfferingRefs(items map[OfferingRef]Offering) []OfferingRef {
	refs := make([]OfferingRef, 0, len(items))
	for ref := range items {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].ServiceID != refs[j].ServiceID {
			return refs[i].ServiceID < refs[j].ServiceID
		}
		return refs[i].WireModelID < refs[j].WireModelID
	})
	return refs
}

func sortedModelKeys(items map[ModelKey]ModelRecord) []ModelKey {
	keys := make([]ModelKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return modelID(keys[i]) < modelID(keys[j])
	})
	return keys
}
