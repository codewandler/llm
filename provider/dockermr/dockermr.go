package dockermr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/provider/providercore"
)

const (
	DefaultBaseURL   = "http://localhost:12434"
	ContainerBaseURL = "http://model-runner.docker.internal"
	defaultEngine    = "llama.cpp"
	engineBaseURL    = DefaultBaseURL + "/engines/" + defaultEngine
)

type Provider struct {
	inner         *providercore.Provider
	client        *http.Client
	modelOnce     sync.Once
	visibleModels llm.Models
}

func New(opts ...llm.Option) *Provider {
	baseOpts := []llm.Option{
		llm.WithBaseURL(engineBaseURL),
		llm.WithAPIKeyFunc(func(context.Context) (string, error) { return "", nil }),
	}
	allOpts := append(baseOpts, opts...)
	llmOpts := llm.Apply(allOpts...)
	client := llmOpts.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	inner := providercore.NewProvider(providercore.NewOptions(
		providercore.WithProviderName(llm.ProviderNameDockerMR),
		providercore.WithBaseURL(engineBaseURL),
		providercore.WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		providercore.WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			models, err := catalogOverlay(ctx, client, llmOpts.BaseURL)
			if err == nil && len(models) > 0 {
				return models, nil
			}
			return curatedModels, nil
		}),
	), allOpts...)

	return &Provider{inner: inner, client: client}
}

func (p *Provider) WithEngine(engine string) *Provider {
	if engine == "" {
		engine = defaultEngine
	}
	newBase := baseURLWithEngine(p.inner.Options().BaseURL, engine)
	return New(llm.WithBaseURL(newBase))
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	endpoint := strings.TrimRight(p.inner.Options().BaseURL, "/") + "/v1/models"
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

func catalogOverlay(ctx context.Context, client *http.Client, baseURL string) (llm.Models, error) {
	base, err := llm.LoadBuiltInCatalog()
	if err != nil {
		return nil, err
	}
	source := catalog.NewDockerMRRuntimeSource()
	source.BaseURL = baseURL
	source.Client = client
	return llm.CatalogVisibleModelsForRuntime(ctx, base, "dockermr-local", source, llm.CatalogModelProjectionOptions{
		ProviderName:          llm.ProviderNameDockerMR,
		ExcludeBuiltinAliases: true,
	})
}
