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
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/api/responses"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const defaultBaseURL = "https://chatgpt.com/backend-api"

type Provider struct {
	auth         *Auth
	opts         *llm.Options
	core         *providercore.Client
	httpClient   *http.Client
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
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = llm.DefaultHttpClient()
	}
	p := &Provider{
		auth:         auth,
		opts:         cfg,
		httpClient:   httpClient,
		defaultModel: DefaultModelID(),
	}
	p.core = p.buildCore()
	return p
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

	resp, err := p.httpClient.Do(req)
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
	return p.core.Stream(ctx, src)
}

func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName: llm.ProviderNameCodex,
		DefaultModel: p.defaultModel,
		BaseURL:      defaultBaseURL,
		// BasePath routes directly to the Codex endpoint, replacing the
		// old transport URL rewrite (/v1/responses → /codex/responses).
		BasePath: "/codex/responses",
		APIHint:  llm.ApiTypeOpenAIResponses,

		// HeaderFunc replaces transport.RoundTrip's auth-header injection.
		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			token, err := p.auth.Token(ctx)
			if err != nil {
				return nil, fmt.Errorf("codex: get token: %w", err)
			}
			return http.Header{
				"Authorization": {"Bearer " + token},
				accountIDHeader: {p.auth.AccountID()},
				codexBetaHeader: {codexBetaValue},
				"originator":    {codexOriginator},
			}, nil
		},

		// MutateRequest replaces injectBodyFields:
		//   - sets "store": false  (prevents Responses API persisting conversations)
		//   - removes max_tokens, max_output_tokens, prompt_cache_retention
		//
		// Note: responses.Request.Store is `bool` with `json:"store,omitempty"`,
		// so false cannot be emitted via the typed struct. Raw JSON mutation is required.
		MutateRequest: func(r *http.Request) {
			if r.Body == nil || r.Header.Get("Content-Type") != "application/json" {
				return
			}
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			payload["store"] = false
			delete(payload, "max_tokens")
			delete(payload, "max_output_tokens")
			delete(payload, "prompt_cache_retention")
			delete(payload, "temperature")
			delete(payload, "top_p")
			delete(payload, "top_k")
			delete(payload, "response_format")
			encoded, err := json.Marshal(payload)
			if err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
		},

		// PreprocessRequest clears effort when thinking is explicitly off
		// (codex models cannot reliably disable reasoning). EffortMax is left
		// as-is here and remapped to "xhigh" in TransformWireRequest after
		// unified conversion, avoiding llm.Effort validation rejecting it.
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			if req.Thinking.IsOff() {
				req.Effort = llm.EffortUnspecified
			}
			return req, original, nil
		},

		// TransformWireRequest remaps EffortMax → "xhigh" on the typed wire
		// object, after unified conversion but before JSON marshalling.
		TransformWireRequest: func(api llm.ApiType, wire any) (any, error) {
			if api != llm.ApiTypeOpenAIResponses {
				return wire, nil
			}
			resp, ok := wire.(*responses.Request)
			if !ok {
				return wire, nil
			}
			if resp.Reasoning != nil && resp.Reasoning.Effort == string(llm.EffortMax) {
				resp.Reasoning.Effort = "xhigh"
			}
			return resp, nil
		},

		TokenCounter: tokencount.TokenCounterFunc(p.CountTokens),
	}
	return providercore.New(cfg, p.coreOptions()...)
}

func (p *Provider) coreOptions() []llm.Option {
	if p.opts == nil {
		return nil
	}
	var opts []llm.Option
	if p.opts.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(p.opts.BaseURL))
	}
	if p.opts.HTTPClient != nil {
		opts = append(opts, llm.WithHTTPClient(p.opts.HTTPClient))
	}
	if p.opts.Logger != nil {
		opts = append(opts, llm.WithLogger(p.opts.Logger))
	}
	return opts
}
