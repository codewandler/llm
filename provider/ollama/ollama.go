package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
	providercore2 "github.com/codewandler/llm/internal/providercore"
)

const (
	ModelGLM47Flash      = "glm-4.7-flash"
	ModelMinistral38B    = "ministral-3:8b"
	ModelRNJ1            = "rnj-1"
	ModelFunctionGemma   = "functiongemma"
	ModelDevstralSmall2  = "devstral-small-2"
	ModelNemotron3Nano30 = "nemotron-3-nano:30b"
	ModelLlama321B       = "llama3.2:1b"
	ModelQwen317B        = "qwen3:1.7b"
	ModelQwen306B        = "qwen3:0.6b"
	ModelGranite31MoE1B  = "granite3.1-moe:1b"
	ModelQwen2505B       = "qwen2.5:0.5b"
	ModelLlama32         = "llama3.2"
	ModelLlama31         = "llama3.1"
	ModelQwen25          = "qwen2.5"
	ModelPhi3            = "phi3"
	ModelDeepSeekR1      = "deepseek-r1"
	ModelMistral         = "mistral"
	ModelGemma3          = "gemma3"
	ModelDefault         = ModelGLM47Flash
	defaultBaseURL       = "http://localhost:11434"
)

type Provider struct {
	inner  *providercore2.Provider
	client *http.Client

	modelOnce     sync.Once
	fetchedModels llm.Models
}

func DefaultOptions() []llm.Option {
	return []llm.Option{llm.WithBaseURL(defaultBaseURL)}
}

func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	llmOpts := llm.Apply(allOpts...)
	client := llmOpts.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	inner := providercore2.NewProvider(providercore2.NewOptions(
		providercore2.WithProviderName(llm.ProviderNameOllama),
		providercore2.WithBaseURL(defaultBaseURL),
		providercore2.WithAPIHint(llm.ApiTypeOpenAIResponses),
		providercore2.WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			models, err := catalogOverlay(ctx, client, llmOpts.BaseURL)
			if err == nil && len(models) > 0 {
				return models, nil
			}
			return curatedModelList, nil
		}),
	), allOpts...)

	return &Provider{inner: inner, client: client}
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}

func (p *Provider) Resolve(modelID string) (llm.Model, error) {
	if m, err := p.Models().Resolve(modelID); err == nil {
		return m, nil
	}
	return llm.Model{ID: modelID, Name: modelID, Provider: llm.ProviderNameOllama}, nil
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	baseURL := p.inner.Options().BaseURL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOllama, resp.StatusCode, string(body))
	}
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]llm.Model, len(result.Models))
	for i, m := range result.Models {
		models[i] = llm.Model{ID: m.Name, Name: m.Name, Provider: llm.ProviderNameOllama}
	}
	return models, nil
}

func (p *Provider) Download(ctx context.Context, models []llm.Model) error {
	installed, err := p.FetchModels(ctx)
	if err != nil {
		return fmt.Errorf("fetch installed models: %w", err)
	}
	installedMap := make(map[string]bool)
	for _, m := range installed {
		installedMap[m.ID] = true
	}
	for _, model := range models {
		if installedMap[model.ID] {
			continue
		}
		if err := p.downloadModel(ctx, model.ID); err != nil {
			return fmt.Errorf("download %s: %w", model.ID, err)
		}
	}
	return nil
}

func (p *Provider) downloadModel(ctx context.Context, modelID string) error {
	reqBody := map[string]string{"name": modelID}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	baseURL := p.inner.Options().BaseURL
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return llm.NewErrRequestFailed(llm.ProviderNameOllama, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return llm.NewErrAPIError(llm.ProviderNameOllama, resp.StatusCode, string(errBody))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var status struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &status); err != nil {
			continue
		}
		if status.Status == "success" || status.Status == "successfully pulled" {
			return nil
		}
		if status.Status != "" && bytes.Contains(bytes.ToLower([]byte(status.Status)), []byte("error")) {
			return fmt.Errorf("pull failed: %s", status.Status)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read pull response: %w", err)
	}
	return nil
}

var curatedModelList = llm.Models{
	{ID: ModelGLM47Flash, Name: "GLM-4.7 Flash", Provider: llm.ProviderNameOllama},
	{ID: ModelMinistral38B, Name: "Ministral 3 8B", Provider: llm.ProviderNameOllama},
	{ID: ModelRNJ1, Name: "RNJ-1", Provider: llm.ProviderNameOllama},
	{ID: ModelFunctionGemma, Name: "FunctionGemma", Provider: llm.ProviderNameOllama},
	{ID: ModelDevstralSmall2, Name: "Devstral Small 2", Provider: llm.ProviderNameOllama},
	{ID: ModelNemotron3Nano30, Name: "Nemotron 3 Nano 30B", Provider: llm.ProviderNameOllama},
	{ID: ModelLlama321B, Name: "Llama 3.2 1B", Provider: llm.ProviderNameOllama},
	{ID: ModelQwen317B, Name: "Qwen 3 1.7B", Provider: llm.ProviderNameOllama},
	{ID: ModelQwen306B, Name: "Qwen 3 0.6B", Provider: llm.ProviderNameOllama},
	{ID: ModelGranite31MoE1B, Name: "Granite 3.1 MoE 1B", Provider: llm.ProviderNameOllama},
	{ID: ModelQwen2505B, Name: "Qwen 2.5 0.5B", Provider: llm.ProviderNameOllama},
}

func catalogOverlay(ctx context.Context, client *http.Client, baseURL string) (llm.Models, error) {
	base, err := llm.LoadBuiltInCatalog()
	if err != nil {
		return nil, err
	}
	source := catalog.NewOllamaRuntimeSource()
	source.BaseURL = baseURL
	source.Client = client
	return llm.CatalogVisibleModelsForRuntime(ctx, base, "ollama-local", source, llm.CatalogModelProjectionOptions{
		ProviderName:          llm.ProviderNameOllama,
		ExcludeBuiltinAliases: true,
	})
}
