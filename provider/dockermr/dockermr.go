// Package dockermr implements the Docker Model Runner (DMR) provider.
//
// Docker Model Runner is built into Docker Desktop 4.40+ and available as a
// plugin for Docker Engine on Linux. It exposes an OpenAI-compatible HTTP API
// for running locally pulled models from Docker Hub's ai/ namespace, backed by
// llama.cpp (default), vLLM (Linux/NVIDIA), or Diffusers (image generation).
//
// This implementation targets the llama.cpp engine and delegates the request
// lifecycle to provider/providercore so that only Docker-specific defaults are
// maintained locally.
package dockermr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const (
	// DefaultBaseURL is the host-side TCP endpoint (Docker Desktop TCP mode or
	// Docker CE on the loopback interface). Available() probes this address for
	// auto-detection.
	DefaultBaseURL = "http://localhost:12434"

	// ContainerBaseURL is accessible from inside Docker Desktop containers.
	ContainerBaseURL = "http://model-runner.docker.internal"

	// defaultEngine is the inference backend path segment used in all API URLs.
	// llama.cpp is the only engine available on all platforms.
	defaultEngine = "llama.cpp"

	// engineBaseURL is the URL prefix that this provider uses as its BaseURL.
	// The Chat Completions client appends "/v1/chat/completions" to this.
	engineBaseURL = DefaultBaseURL + "/engines/" + defaultEngine

	completionsPath = "/v1/chat/completions"
)

// Provider implements the Docker Model Runner LLM backend.
type Provider struct {
	core          *providercore.Client
	opts          *llm.Options
	client        *http.Client
	defaultModel  string
	models        llm.Models
	modelOnce     sync.Once
	visibleModels llm.Models
}

// New creates a Docker Model Runner provider.
//
// Callers may pass any llm.Option to override the defaults (base URL, HTTP
// client, etc.). To target the inside-container address, pass:
//
//	dockermr.New(llm.WithBaseURL(dockermr.ContainerBaseURL + "/engines/llama.cpp"))
func New(opts ...llm.Option) *Provider {
	baseOpts := []llm.Option{
		llm.WithBaseURL(engineBaseURL),
		llm.WithAPIKeyFunc(func(context.Context) (string, error) { return "", nil }),
	}
	allOpts := append(baseOpts, opts...)

	llmOpts := llm.Apply(allOpts...)
	httpClient := llmOpts.HTTPClient
	if httpClient == nil {
		httpClient = llm.DefaultHttpClient()
	}

	core := newDockermrCore(ModelDefault, allOpts...)

	return &Provider{
		core:         core,
		opts:         llmOpts,
		client:       httpClient,
		defaultModel: ModelDefault,
		models:       curatedModels,
	}
}

// WithEngine returns a copy of the provider configured to target a different
// inference engine path (e.g. "vllm").
func (p *Provider) WithEngine(engine string) *Provider {
	if engine == "" {
		engine = defaultEngine
	}

	clone := *p
	optsCopy := *p.opts
	optsCopy.BaseURL = baseURLWithEngine(p.opts.BaseURL, engine)
	clone.opts = &optsCopy
	clone.client = optsCopy.HTTPClient
	if clone.client == nil {
		clone.client = llm.DefaultHttpClient()
	}
	clone.modelOnce = sync.Once{}
	clone.visibleModels = nil
	clone.core = newDockermrCore(clone.defaultModel, optionsFromOptions(&optsCopy)...)
	return &clone
}

// Name returns the provider identifier used in error messages and usage records.
func (p *Provider) Name() string { return llm.ProviderNameDockerMR }

// CostCalculator returns a no-op cost calculator (DMR has no pricing).
func (p *Provider) CostCalculator() usage.CostCalculator {
	return usage.CostCalculatorFunc(func(_, _ string, _ usage.TokenItems) (usage.Cost, bool) {
		return usage.Cost{}, false
	})
}

// Models returns the visible Docker Model Runner model view.
// When runtime discovery succeeds it includes locally pulled models plus known
// pullable ai/ models from the catalog overlay. On failure it falls back to the
// curated visible list.
func (p *Provider) Models() llm.Models {
	p.modelOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		models, err := p.catalogModels(ctx)
		if err == nil && len(models) > 0 {
			p.visibleModels = models
		}
	})
	if p.visibleModels != nil {
		return p.visibleModels
	}
	return p.models
}

// Resolve looks up a model by ID or alias in the visible model list.
func (p *Provider) Resolve(modelID string) (llm.Model, error) { return p.Models().Resolve(modelID) }

func (p *Provider) catalogModels(ctx context.Context) (llm.Models, error) {
	base, err := llm.LoadBuiltInCatalog()
	if err != nil {
		return nil, err
	}
	source := catalog.NewDockerMRRuntimeSource()
	source.BaseURL = p.opts.BaseURL
	source.Client = p.client
	return llm.CatalogVisibleModelsForRuntime(ctx, base, "dockermr-local", source, llm.CatalogModelProjectionOptions{
		ProviderName:          p.Name(),
		ExcludeBuiltinAliases: true,
	})
}

// FetchModels queries the DMR endpoint for the list of locally pulled models.
func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	endpoint := strings.TrimRight(p.opts.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dockermr list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameDockerMR, resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]llm.Model, len(result.Data))
	for i, m := range result.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models[i] = llm.Model{ID: m.ID, Name: name, Provider: llm.ProviderNameDockerMR}
	}
	return models, nil
}

// CreateStream delegates to the shared providercore client.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.core.Stream(ctx, src)
}

// CountTokens estimates the number of input tokens using heuristic overheads
// equivalent to OpenAI's chat tokenizer (4 tokens per message + 3 reply priming).
func (p *Provider) CountTokens(_ context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	return estimateTokens(p.defaultModel, req)
}

func baseURLWithEngine(base, engine string) string {
	const marker = "/engines/"
	if base == "" {
		return DefaultBaseURL + marker + engine
	}
	idx := strings.Index(base, marker)
	if idx == -1 {
		return strings.TrimRight(base, "/") + marker + engine
	}

	start := idx + len(marker)
	rest := base[start:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		return base[:start] + engine + rest[slash:]
	}
	return base[:start] + engine
}

func estimateTokens(defaultModel string, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
	model := req.Model
	if model == "" {
		model = defaultModel
	}

	enc, _ := tokencount.EncodingForModel(model)
	if enc == "" {
		enc = tokencount.EncodingCL100K
	}

	tc := &tokencount.TokenCount{}
	if err := tokencount.CountMessagesAndTools(tc, tokencount.TokenCountRequest{
		Model:    model,
		Messages: req.Messages,
		Tools:    req.Tools,
	}, tokencount.CountOpts{
		Encoding:       enc,
		PerMsgOverhead: 4,
		ReplyPriming:   3,
	}); err != nil {
		return nil, fmt.Errorf("dockermr: %w", err)
	}
	return tc, nil
}

func newDockermrCore(defaultModel string, opts ...llm.Option) *providercore.Client {
	cfg := providercore.Config{
		ProviderName: llm.ProviderNameDockerMR,
		DefaultModel: defaultModel,
		BaseURL:      engineBaseURL,
		BasePath:     completionsPath,
		APIHint:      llm.ApiTypeOpenAIChatCompletion,
	}
	providercore.WithCostCalculator(nil)(&cfg)
	cfg.TokenCounter = tokencount.TokenCounterFunc(func(ctx context.Context, req tokencount.TokenCountRequest) (*tokencount.TokenCount, error) {
		return estimateTokens(defaultModel, req)
	})
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
