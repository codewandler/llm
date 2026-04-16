package modelsdev

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const DefaultURL = "https://models.dev/api.json"

type Database map[string]Provider

type Provider struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Env    []string         `json:"env,omitempty"`
	NPM    string           `json:"npm,omitempty"`
	API    string           `json:"api,omitempty"`
	Doc    string           `json:"doc,omitempty"`
	Models map[string]Model `json:"models"`
}

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

type Interleaved struct {
	Enabled bool
	Field   string
}

func (i *Interleaved) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		i.Enabled = b
		return nil
	}
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

func (i Interleaved) MarshalJSON() ([]byte, error) {
	if i.Field != "" {
		return json.Marshal(struct {
			Field string `json:"field"`
		}{Field: i.Field})
	}
	return json.Marshal(i.Enabled)
}

type Modalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

type Cost struct {
	Input           float64 `json:"input,omitempty"`
	Output          float64 `json:"output,omitempty"`
	CacheRead       float64 `json:"cache_read,omitempty"`
	CacheWrite      float64 `json:"cache_write,omitempty"`
	ContextOver200k *Cost   `json:"context_over_200k,omitempty"`
}

type Limit struct {
	Context int `json:"context,omitempty"`
	Output  int `json:"output,omitempty"`
}

var (
	fixturePath     string
	fixturePathOnce sync.Once
)

func DefaultFixturePath() string {
	fixturePathOnce.Do(func() {
		_, currentFile, _, _ := runtime.Caller(0)
		fixturePath = filepath.Join(filepath.Dir(currentFile), "testdata", "api.json")
	})
	return fixturePath
}

func LoadFile(path string) (Database, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data)
}

func LoadBytes(data []byte) (Database, error) {
	var db Database
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("decode models.dev payload: %w", err)
	}
	return db, nil
}
