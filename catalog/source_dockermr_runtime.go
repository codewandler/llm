package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultDockerMRBaseURL = "http://localhost:12434/engines/llama.cpp"

type DockerMRRuntimeSource struct {
	BaseURL     string
	Client      *http.Client
	RuntimeID   string
	RuntimeName string
	KnownModels []KnownRuntimeModel
}

func NewDockerMRRuntimeSource() DockerMRRuntimeSource {
	return DockerMRRuntimeSource{}
}

func (DockerMRRuntimeSource) ID() string { return "dockermr-runtime" }

func (s DockerMRRuntimeSource) Fetch(ctx context.Context) (*Fragment, error) {
	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultDockerMRBaseURL
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	observedAt := time.Now().UTC()
	runtimeID := firstNonEmpty(s.RuntimeID, "dockermr-local")
	runtimeName := firstNonEmpty(s.RuntimeName, "Docker Model Runner Local")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dockermr runtime source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("dockermr runtime source: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	known := s.KnownModels
	if known == nil {
		known = defaultDockerMRKnownModels()
	}
	installed := make(map[string]string, len(payload.Data))
	for _, item := range payload.Data {
		installed[item.ID] = firstNonEmpty(item.Name, item.ID)
	}

	models := make(map[ModelKey]ModelRecord)
	offerings := make(map[OfferingRef]Offering)
	access := make(map[OfferingRef]RuntimeAccess)
	acquisition := make(map[OfferingRef]RuntimeAcquisition)

	service := Service{
		ID:       "dockermr",
		Name:     "Docker Model Runner",
		Kind:     ServiceKindLocal,
		Operator: "docker",
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
		acquisition[ref] = RuntimeAcquisition{
			RuntimeID:  runtimeID,
			Offering:   ref,
			Known:      true,
			Acquirable: true,
			Status:     "pullable",
			Action:     "pull",
			Provenance: []Provenance{{SourceID: s.ID(), Authority: string(AuthorityLocal), ObservedAt: observedAt, RawID: model.ID}},
		}
	}

	for modelID, name := range installed {
		ref, record, offering := localRuntimeEntries(service.ID, modelID, name, s.ID(), observedAt)
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

func defaultDockerMRKnownModels() []KnownRuntimeModel {
	return []KnownRuntimeModel{
		{ID: "ai/smollm2", Name: "SmolLM2 360M"},
		{ID: "ai/smollm2:135M-Q4_K_M", Name: "SmolLM2 135M"},
		{ID: "ai/qwen2.5:0.5B-F16", Name: "Qwen2.5 0.5B"},
		{ID: "ai/qwen2.5", Name: "Qwen2.5 7B"},
		{ID: "ai/qwen3", Name: "Qwen3"},
		{ID: "ai/qwen3-coder", Name: "Qwen3 Coder"},
		{ID: "ai/llama3.2", Name: "Llama 3.2"},
		{ID: "ai/llama3.3", Name: "Llama 3.3"},
		{ID: "ai/phi4-mini", Name: "Phi-4 Mini"},
		{ID: "ai/phi4", Name: "Phi-4"},
		{ID: "ai/gemma3", Name: "Gemma 3"},
		{ID: "ai/gemma4", Name: "Gemma 4"},
		{ID: "ai/deepseek-r1", Name: "DeepSeek R1"},
		{ID: "ai/mistral-small3.2", Name: "Mistral Small 3.2"},
		{ID: "ai/glm-4.7-flash", Name: "GLM-4.7 Flash"},
		{ID: "ai/granite4.0-nano", Name: "Granite 4.0 Nano"},
		{ID: "ai/functiongemma", Name: "FunctionGemma"},
	}
}
