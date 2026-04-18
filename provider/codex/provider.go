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

	"github.com/codewandler/llm"
	providercore2 "github.com/codewandler/llm/internal/providercore"
)

const defaultBaseURL = "https://chatgpt.com/backend-api"

type Provider struct {
	auth       *Auth
	opts       *llm.Options
	inner      *providercore2.Provider
	httpClient *http.Client
	modelOnce  sync.Once
	models     llm.Models
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
		auth:       auth,
		opts:       cfg,
		httpClient: httpClient,
	}

	p.inner = providercore2.NewProvider(providercore2.NewOptions(
		providercore2.WithProviderName(llm.ProviderNameCodex),
		providercore2.WithBaseURL(defaultBaseURL),
		providercore2.WithBasePath("/codex/responses"),
		providercore2.WithAPIHint(llm.ApiTypeOpenAIResponses),
		providercore2.WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			return p.Models(), nil
		}),
		providercore2.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			token, err := auth.Token(ctx)
			if err != nil {
				return nil, fmt.Errorf("codex: get token: %w", err)
			}
			return http.Header{
				"Authorization": {"Bearer " + token},
				accountIDHeader: {auth.AccountID()},
				codexBetaHeader: {codexBetaValue},
				"originator":    {codexOriginator},
			}, nil
		}),
		providercore2.WithMutateRequest(func(r *http.Request) {
			if r.Body == nil || r.Header.Get("Content-Type") != "application/json" {
				return
			}
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				return
			}
			payload["store"] = false
			// The Codex API does not accept these parameters; strip them
			// from the wire body so the request is not rejected.
			// NOTE: prompt_cache_retention IS supported and must be kept.
			delete(payload, "max_tokens")
			delete(payload, "max_output_tokens")
			delete(payload, "temperature")
			delete(payload, "top_p")
			delete(payload, "top_k")
			delete(payload, "response_format")
			encoded, err := json.Marshal(payload)
			if err != nil {
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
		}),
		providercore2.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			// Map effort for the Codex API. All Codex models support the
			// same reasoning effort levels (low, medium, high, xhigh).
			// Unlike the blunt "clear effort when thinking is off" approach,
			// we preserve effort regardless of ThinkingMode because the
			// Codex API treats reasoning depth (effort) and thinking
			// visibility as independent concerns.
			// EffortMax → xhigh is handled by the ResponsesRequestTransform.
			req.Effort = mapCodexEffort(req.Effort, req.Thinking)

			hasSystem := false
			for _, m := range req.Messages {
				if m.IsSystem() {
					hasSystem = true
					break
				}
			}
			if !hasSystem {
				req.Messages = append(llm.Messages{llm.System("You are a helpful assistant.")}, req.Messages...)
			}
			return req, original, nil
		}),
		providercore2.WithResponsesRequestTransform(func(resp *providercore2.ResponsesRequest) error {
			if resp.Reasoning != nil && resp.Reasoning.Effort == string(llm.EffortMax) {
				resp.Reasoning.Effort = "xhigh"
			}
			// Codex backend requires explicit summary to stream reasoning text.
			// Standard OpenAI API does this by default; Codex defaults to "none".
			if resp.Reasoning != nil && resp.Reasoning.Summary == "" {
				resp.Reasoning.Summary = "auto"
			}
			return nil
		}),
	), allOpts...)

	return p
}

func (p *Provider) Name() string { return p.inner.Name() }

func (p *Provider) Models() llm.Models {
	p.modelOnce.Do(func() {
		p.models = fallbackModels()
	})
	return p.models
}

func (p *Provider) FetchRawModels(ctx context.Context) ([]byte, error) {
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
	return io.ReadAll(resp.Body)
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	raw, err := p.FetchRawModels(ctx)
	if err != nil {
		return nil, err
	}
	var payload modelsResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
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
	return p.inner.CreateStream(ctx, src)
}

// mapCodexEffort maps the user-requested Effort and ThinkingMode to a valid
// Codex reasoning_effort value. All Codex models support the same levels:
// low, medium, high, xhigh (mapped from EffortMax by ResponsesRequestTransform).
//
// Unlike the legacy approach that cleared effort when thinking was off, this
// function preserves effort regardless of ThinkingMode. The Codex API treats
// reasoning depth (effort) and thinking visibility as independent concerns —
// a user can request high effort without visible thinking output.
func mapCodexEffort(effort llm.Effort, thinking llm.ThinkingMode) llm.Effort {
	// Thinking explicitly on but no effort specified → default to high.
	if thinking.IsOn() && effort.IsEmpty() {
		return llm.EffortHigh
	}

	// All other cases: pass effort through as-is.
	// - ThinkingOff + effort set → preserve effort (reasoning depth is independent).
	// - ThinkingOff + no effort  → omit, let API use its default (medium).
	// - ThinkingAuto + effort    → preserve effort.
	// - No effort at all         → omit, let API use its default.
	return effort
}