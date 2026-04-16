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
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

// Known model IDs for Ollama.
// These are tested and known to work well with the Responses API.
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
)

const (
	ModelDefault   = ModelGLM47Flash
	defaultBaseURL = "http://localhost:11434"
	responsesPath  = "/v1/responses"
)

// Provider implements the Ollama (local) LLM backend.
type Provider struct {
	core         *providercore.Client
	opts         *llm.Options
	defaultModel string
	client       *http.Client
	// lazy model list — populated on first Models() call
	modelOnce     sync.Once
	fetchedModels llm.Models
}

// DefaultOptions returns the default options for Ollama.
// Base URL defaults to http://localhost:11434.
// No API key is required for Ollama.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
	}
}

// New creates a new Ollama provider.
// Options are applied on top of DefaultOptions().
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	p := &Provider{
		opts:         cfg,
		defaultModel: ModelDefault,
		client:       client,
	}
	p.core = newOllamaCore(p.defaultModel, allOpts...)
	return p
}

// WithDefaultModel sets the default model to use.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	p.core = newOllamaCore(modelID, optionsFromOptions(p.opts)...)
	return p
}

// DefaultModel returns the configured default model ID.
func (p *Provider) DefaultModel() string {
	return p.defaultModel
}

func (p *Provider) Name() string { return llm.ProviderNameOllama }

func (*Provider) CostCalculator() usage.CostCalculator {
	// Ollama is local; no cost information is available.
	return usage.CostCalculatorFunc(func(_, _ string, _ usage.TokenItems) (usage.Cost, bool) {
		return usage.Cost{}, false
	})
}

// Models returns the visible Ollama model view.
// When runtime discovery succeeds it includes installed models plus known
// pullable models from the catalog overlay. On failure it falls back to a
// curated visible list of well-tested models.
func (p *Provider) Models() llm.Models {
	p.modelOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		models, err := p.catalogModels(ctx)
		if err == nil && len(models) > 0 {
			p.fetchedModels = models
		}
	})
	if p.fetchedModels != nil {
		return p.fetchedModels
	}
	return p.curatedModels()
}

func (p *Provider) catalogModels(ctx context.Context) (llm.Models, error) {
	base, err := llm.LoadBuiltInCatalog()
	if err != nil {
		return nil, err
	}
	source := catalog.NewOllamaRuntimeSource()
	source.BaseURL = p.opts.BaseURL
	source.Client = p.client
	return llm.CatalogVisibleModelsForRuntime(ctx, base, "ollama-local", source, llm.CatalogModelProjectionOptions{
		ProviderName:          p.Name(),
		ExcludeBuiltinAliases: true,
	})
}

// curatedModels returns a static list of tested models that are known to work
// with streaming, tool calling, and conversations. Used as a fallback when the
// live list cannot be fetched.
func (p *Provider) curatedModels() llm.Models {
	providerName := p.Name()
	return llm.Models{
		{ID: ModelGLM47Flash, Name: "GLM-4.7 Flash", Provider: providerName},
		{ID: ModelMinistral38B, Name: "Ministral 3 8B", Provider: providerName},
		{ID: ModelRNJ1, Name: "RNJ-1", Provider: providerName},
		{ID: ModelFunctionGemma, Name: "FunctionGemma", Provider: providerName},
		{ID: ModelDevstralSmall2, Name: "Devstral Small 2", Provider: providerName},
		{ID: ModelNemotron3Nano30, Name: "Nemotron 3 Nano 30B", Provider: providerName},
		{ID: ModelLlama321B, Name: "Llama 3.2 1B", Provider: providerName},
		{ID: ModelQwen317B, Name: "Qwen 3 1.7B", Provider: providerName},
		{ID: ModelQwen306B, Name: "Qwen 3 0.6B", Provider: providerName},
		{ID: ModelGranite31MoE1B, Name: "Granite 3.1 MoE 1B", Provider: providerName},
		{ID: ModelQwen2505B, Name: "Qwen 2.5 0.5B", Provider: providerName},
	}
}

// Resolve returns the model matching modelID.
// It searches the live/curated model list first; if not found it returns a
// synthetic Model so that any locally-installed Ollama model can be addressed
// by ID without needing to be in the curated list.
func (p *Provider) Resolve(modelID string) (llm.Model, error) {
	if m, err := p.Models().Resolve(modelID); err == nil {
		return m, nil
	}
	// Pass through — Ollama serves any locally-installed model.
	return llm.Model{ID: modelID, Name: modelID, Provider: p.Name()}, nil
}

// FetchModels retrieves the list of currently installed models from Ollama.
// This enumerates ALL models, including ones that may not support chat.
func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.opts.BaseURL+"/api/tags", nil)
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
	providerName := p.Name()
	for i, m := range result.Models {
		models[i] = llm.Model{
			ID:       m.Name,
			Name:     m.Name,
			Provider: providerName,
		}
	}
	return models, nil
}

// Download pulls the specified models from the Ollama registry.
// This method blocks until all models are downloaded.
// Models that are already installed will be skipped.
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

// downloadModel pulls a single model from the Ollama registry.
func (p *Provider) downloadModel(ctx context.Context, modelID string) error {
	reqBody := map[string]string{"name": modelID}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.opts.BaseURL+"/api/pull", bytes.NewReader(body))
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

// CreateStream sends the request to Ollama's OpenAI-compatible Responses API.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.core.Stream(ctx, src)
}

func newOllamaCore(defaultModel string, opts ...llm.Option) *providercore.Client {
	cfg := providercore.Config{
		ProviderName: llm.ProviderNameOllama,
		DefaultModel: defaultModel,
		BaseURL:      defaultBaseURL,
		BasePath:     responsesPath,
		APIHint:      llm.ApiTypeOpenAIResponses,
		TokenCounter: tokencount.TokenCounterFunc(func(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
			tc := &tokencount.TokenCount{}
			if err := tokencount.CountMessagesAndTools(tc, req, tokencount.CountOpts{Encoding: tokencount.EncodingCL100K}); err != nil {
				return nil, fmt.Errorf("ollama: %w", err)
			}
			return tc, nil
		}),
	}
	providercore.WithCostCalculator(nil)(&cfg)
	return providercore.New(cfg, opts...)
}

func optionsFromOptions(o *llm.Options) []llm.Option {
	if o == nil {
		return nil
	}
	var opts []llm.Option
	if o.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(o.BaseURL))
	}
	if o.HTTPClient != nil {
		opts = append(opts, llm.WithHTTPClient(o.HTTPClient))
	}
	if o.APIKeyFunc != nil {
		opts = append(opts, llm.WithAPIKeyFunc(o.APIKeyFunc))
	}
	if o.Logger != nil {
		opts = append(opts, llm.WithLogger(o.Logger))
	}
	return opts
}
