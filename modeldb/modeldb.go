// Package modeldb provides access to the models.dev model database.
//
// This package embeds the full models.dev database for use by external consumers
// (CLI tools, UIs, model selectors). It is NOT used by provider implementations
// for pricing calculations - providers maintain their own pricing logic.
//
// To update the embedded model database, run:
//
//	curl -sL https://models.dev/api.json -o modeldb/api.json
package modeldb

import (
	"embed"
	"encoding/json"
	"sync"
)

//go:embed api.json
var dataFS embed.FS

var (
	db     Database
	dbOnce sync.Once
	dbErr  error
)

// ProviderMapping maps internal provider names to models.dev provider IDs.
var ProviderMapping = map[string]string{
	"bedrock":    "amazon-bedrock",
	"anthropic":  "anthropic",
	"openai":     "openai",
	"openrouter": "openrouter",
	"google":     "google",
}

// Database is the root structure mapping provider IDs to providers.
type Database map[string]Provider

// Provider represents an LLM provider with its models.
type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Env    []string         `json:"env,omitempty"`
	NPM    string           `json:"npm,omitempty"`
	API    string           `json:"api,omitempty"`
	Doc    string           `json:"doc,omitempty"`
	Models map[string]Model `json:"models"`
}

// Model represents a single LLM model with metadata and pricing.
type Model struct {
	ID               string       `json:"id"`
	Name             string       `json:"name"`
	Family           string       `json:"family,omitempty"`
	Attachment       bool         `json:"attachment,omitempty"`
	Reasoning        bool         `json:"reasoning,omitempty"`
	ToolCall         bool         `json:"tool_call,omitempty"`
	StructuredOutput bool         `json:"structured_output,omitempty"`
	Temperature      bool         `json:"temperature,omitempty"`
	Interleaved      *Interleaved `json:"interleaved,omitempty"`
	Knowledge        string       `json:"knowledge,omitempty"`
	ReleaseDate      string       `json:"release_date,omitempty"`
	LastUpdated      string       `json:"last_updated,omitempty"`
	Modalities       Modalities   `json:"modalities,omitempty"`
	OpenWeights      bool         `json:"open_weights,omitempty"`
	Cost             Cost         `json:"cost,omitempty"`
	Limit            Limit        `json:"limit,omitempty"`
}

// Interleaved describes reasoning interleaving configuration.
// Can be unmarshaled from boolean true or object {"field": "reasoning_content"}.
type Interleaved struct {
	Enabled bool
	Field   string
}

// UnmarshalJSON handles both boolean and object formats for interleaved config.
func (i *Interleaved) UnmarshalJSON(data []byte) error {
	// Try bool first
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		i.Enabled = b
		return nil
	}

	// Try object with field
	var obj struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	i.Enabled = true
	i.Field = obj.Field
	return nil
}

// MarshalJSON serializes Interleaved back to JSON.
func (i Interleaved) MarshalJSON() ([]byte, error) {
	if i.Field != "" {
		return json.Marshal(struct {
			Field string `json:"field"`
		}{Field: i.Field})
	}
	return json.Marshal(i.Enabled)
}

// Modalities describes supported input/output types.
type Modalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

// Cost represents pricing in USD per 1M tokens.
type Cost struct {
	Input           float64 `json:"input,omitempty"`
	Output          float64 `json:"output,omitempty"`
	CacheRead       float64 `json:"cache_read,omitempty"`
	CacheWrite      float64 `json:"cache_write,omitempty"`
	ContextOver200k *Cost   `json:"context_over_200k,omitempty"`
}

// Limit describes context and output token limits.
type Limit struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

// Load returns the parsed database, loading lazily on first call.
// Thread-safe via sync.Once. Returns the same instance on subsequent calls.
func Load() (Database, error) {
	dbOnce.Do(func() {
		data, err := dataFS.ReadFile("api.json")
		if err != nil {
			dbErr = err
			return
		}
		dbErr = json.Unmarshal(data, &db)
	})
	return db, dbErr
}

// MustLoad returns the database or panics if loading fails.
// Intended for use at init time when failure is unrecoverable.
func MustLoad() Database {
	db, err := Load()
	if err != nil {
		panic("modeldb: " + err.Error())
	}
	return db
}

// GetProvider returns the provider for the given internal name.
// Uses ProviderMapping to translate internal names to models.dev IDs.
func GetProvider(name string) (Provider, bool) {
	db, err := Load()
	if err != nil {
		return Provider{}, false
	}

	// Try mapping first
	if mapped, ok := ProviderMapping[name]; ok {
		name = mapped
	}

	p, ok := db[name]
	return p, ok
}

// GetModel returns a model by provider name and model ID.
func GetModel(providerName, modelID string) (Model, bool) {
	p, ok := GetProvider(providerName)
	if !ok {
		return Model{}, false
	}
	m, ok := p.Models[modelID]
	return m, ok
}

// Providers returns a list of all provider IDs in the database.
func Providers() []string {
	db, err := Load()
	if err != nil {
		return nil
	}

	providers := make([]string, 0, len(db))
	for id := range db {
		providers = append(providers, id)
	}
	return providers
}
