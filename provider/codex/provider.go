package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const defaultBaseURL = "https://chatgpt.com/backend-api"

type Provider struct {
	auth         *Auth
	opts         *llm.Options
	client       *http.Client
	defaultModel string
	modelOnce    sync.Once
	models       llm.Models
}

func DefaultOptions() []llm.Option {
	return []llm.Option{llm.WithBaseURL(defaultBaseURL)}
}

func New(auth *Auth, opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	baseClient := cfg.HTTPClient
	if baseClient == nil {
		baseClient = llm.DefaultHttpClient()
	}
	clientCopy := *baseClient
	baseTransport := clientCopy.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	clientCopy.Transport = &transport{base: baseTransport, auth: auth}
	return &Provider{
		auth:         auth,
		opts:         cfg,
		client:       &clientCopy,
		defaultModel: DefaultModelID(),
	}
}

func (p *Provider) Name() string { return llm.ProviderNameCodex }

func (*Provider) CostCalculator() usage.CostCalculator { return usage.Default() }

func (p *Provider) Resolve(modelID string) (llm.Model, error) { return p.Models().Resolve(modelID) }

func (p *Provider) Models() llm.Models {
	p.modelOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		resolved, err := llm.ResolveCatalog(ctx, catalog.RegisteredSource{
			Stage:     catalog.StageRuntime,
			Authority: catalog.AuthorityLocal,
			Source:    NewRuntimeSource(),
		})
		if err == nil {
			models := llm.CatalogModelsForRuntime(resolved, runtimeID, false, llm.CatalogModelProjectionOptions{
				ProviderName:          p.Name(),
				ExcludeBuiltinAliases: true,
			})
			if len(models) > 0 {
				p.models = attachProviderAliases(models)
				return
			}
		}
		p.models = fallbackModels()
	})
	return p.models
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	token, err := p.auth.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("codex: get token: %w", err)
	}
	endpoint := strings.TrimRight(p.opts.BaseURL, "/") + "/codex/models?client_version=" + modelsClientVersion
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("codex: create models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(accountIDHeader, p.auth.AccountID())
	req.Header.Set(codexBetaHeader, codexBetaValue)
	req.Header.Set("originator", codexOriginator)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(body))
	}
	var payload modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]llm.Model, 0, len(payload.Models))
	for _, model := range payload.Models {
		models = append(models, llm.Model{
			ID:       model.Slug,
			Name:     firstNonEmpty(model.DisplayName, model.Slug),
			Provider: p.Name(),
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	reqOpts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}
	if reqOpts.Model == "" || reqOpts.Model == llm.ModelDefault {
		reqOpts.Model = p.defaultModel
	}
	enriched, err := openai.EnrichRequest(reqOpts)
	if err != nil {
		return nil, err
	}
	body, err := openai.BuildResponsesBody(enriched)
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.opts.BaseURL, "/")+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}
	req.Header.Set("Content-Type", "application/json")

	pub, ch := llm.NewEventPublisher()
	pub.Publish(&llm.RequestEvent{
		OriginalRequest: reqOpts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: llm.ApiTypeOpenAIResponses,
	})
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{Model: reqOpts.Model, Messages: reqOpts.Messages, Tools: reqOpts.Tools}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, p.Name(), reqOpts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	startTime := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(p.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		pub.Close()
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(errBody))
	}

	go openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
		RequestedModel: reqOpts.Model,
		StartTime:      startTime,
		ProviderName:   p.Name(),
		Logger:         p.opts.Logger,
	})
	return ch, nil
}

type transport struct {
	base http.RoundTripper
	auth *Auth
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if strings.HasSuffix(req.URL.Path, "/v1/responses") {
		req.URL.Path = strings.TrimSuffix(req.URL.Path, "/v1/responses") + "/codex/responses"
	}
	token, err := t.auth.Token(req.Context())
	if err != nil {
		return nil, fmt.Errorf("codex transport: get token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(accountIDHeader, t.auth.AccountID())
	req.Header.Set(codexBetaHeader, codexBetaValue)
	req.Header.Set("originator", codexOriginator)
	if req.Body != nil && req.Header.Get("Content-Type") == "application/json" {
		body, length, err := injectBodyFields(req.Body)
		if err == nil {
			req.Body = body
			req.ContentLength = length
		}
	}
	return t.base.RoundTrip(req)
}

func injectBodyFields(body io.ReadCloser) (io.ReadCloser, int64, error) {
	defer body.Close()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, 0, fmt.Errorf("decode body: %w", err)
	}
	payload["store"] = false
	delete(payload, "max_tokens")
	delete(payload, "max_output_tokens")
	delete(payload, "prompt_cache_retention")
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("encode body: %w", err)
	}
	return io.NopCloser(bytes.NewReader(encoded)), int64(len(encoded)), nil
}
