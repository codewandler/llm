package openrouter

import (
	_ "embed"
	"encoding/json"

	"github.com/codewandler/llm"
)

//go:embed models.json
var modelsJSON []byte

// ModelData represents the full structure of a model from OpenRouter API
type ModelData struct {
	ID            string `json:"id"`
	CanonicalSlug string `json:"canonical_slug"`
	HuggingFaceID string `json:"hugging_face_id"`
	Name          string `json:"name"`
	Created       int64  `json:"created"`
	Description   string `json:"description"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		Modality         string   `json:"modality"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
		Tokenizer        string   `json:"tokenizer"`
		InstructType     *string  `json:"instruct_type"`
	} `json:"architecture"`
	Pricing struct {
		Prompt         string `json:"prompt"`
		Completion     string `json:"completion"`
		InputCacheRead string `json:"input_cache_read"`
	} `json:"pricing"`
	TopProvider struct {
		ContextLength       int  `json:"context_length"`
		MaxCompletionTokens int  `json:"max_completion_tokens"`
		IsModerated         bool `json:"is_moderated"`
	} `json:"top_provider"`
	PerRequestLimits    interface{}            `json:"per_request_limits"`
	SupportedParameters []string               `json:"supported_parameters"`
	DefaultParameters   map[string]interface{} `json:"default_parameters"`
}

// loadEmbeddedModels loads the curated list of tool-enabled models
// from the embedded models.json file.
func loadEmbeddedModels() []llm.Model {
	var models []ModelData

	if err := json.Unmarshal(modelsJSON, &models); err != nil {
		// If we can't parse the embedded file, return empty list
		return nil
	}

	result := make([]llm.Model, len(models))
	for i, m := range models {
		result[i] = llm.Model{
			ID:       m.ID,
			Name:     m.Name,
			Provider: providerName,
		}
	}

	return result
}

// GetModelData returns the full model data from the embedded models.json file.
// This includes pricing, context length, supported parameters, and other metadata.
func GetModelData() ([]ModelData, error) {
	var models []ModelData
	if err := json.Unmarshal(modelsJSON, &models); err != nil {
		return nil, err
	}
	return models, nil
}
