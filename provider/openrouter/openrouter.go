package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/providercore"
)

const (
	defaultBaseURL = "https://openrouter.ai/api"
	providerName   = "openrouter"

	DefaultModel = "auto"
)

type Provider struct {
	inner        *providercore.Provider
	opts         *llm.Options
	client       *http.Client
	defaultModel string
	models       llm.Models
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

	models := catalogModels()

	p := &Provider{
		opts:         llmOpts,
		client:       client,
		defaultModel: DefaultModel,
		models:       models,
	}

	p.inner = providercore.NewProvider(providercore.NewOptions(
		providercore.WithProviderName(providerName),
		providercore.WithBaseURL(defaultBaseURL),
		providercore.WithAPIHintResolver(func(req llm.Request) llm.ApiType {
			_, hint := selectAPI(p.normalizeRequestModel(req.Model), req.ApiTypeHint)
			return hint
		}),
		providercore.WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			return p.models, nil
		}),
		providercore.WithHeaderFunc(func(ctx context.Context, req *llm.Request) (http.Header, error) {
			key, err := p.opts.ResolveAPIKey(ctx)
			if err != nil {
				return nil, err
			}
			if key == "" {
				return nil, llm.NewErrMissingAPIKey(providerName)
			}
			return http.Header{"Authorization": {"Bearer " + key}}, nil
		}),
		providercore.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			normalized := p.normalizeRequestModel(req.Model)
			req.Model = normalized

			backend, resolved := selectAPI(normalized, req.ApiTypeHint)
			req.ApiTypeHint = resolved
			if backend == orMessages {
				req.Model = strings.TrimPrefix(normalized, "anthropic/")
			}

			// Responses API doesn't support thinking - strip it from messages
			if backend == orResponses {
				filteredMessages := make(llm.Messages, 0, len(req.Messages))
				for _, msg := range req.Messages {
					filteredMessages = append(filteredMessages, stripThinkingParts(msg))
				}
				req.Messages = filteredMessages
			}

			return req, original, nil
		}),
		providercore.WithMutateRequest(func(r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/v1/messages") {
				r.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
				r.Header.Set("Anthropic-Beta", anthropic.BetaInterleavedThinking)
			}
		}),
	), allOpts...)

	return p
}

func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	return p
}

func (p *Provider) DefaultModel() string { return p.defaultModel }
func (p *Provider) Name() string         { return p.inner.Name() }
func (p *Provider) Models() llm.Models   { return p.inner.Models() }

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOpenRouter, resp.StatusCode, string(body))
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
		models[i] = llm.Model{ID: m.ID, Name: m.Name, Provider: providerName}
	}
	return models, nil
}

func catalogModels() llm.Models {
	c, err := llm.LoadBuiltInCatalog()
	if err == nil {
		models := llm.CatalogModelsForService(c, providerName, llm.CatalogModelProjectionOptions{
			ProviderName:          providerName,
			ExcludeBuiltinAliases: true,
		})
		if len(models) > 0 {
			return ensureOpenRouterAliases(models)
		}
	}
	return loadEmbeddedModels()
}

func ensureOpenRouterAliases(models llm.Models) llm.Models {
	aliases := []string{llm.ModelDefault, "auto", llm.ModelFast}
	for i := range models {
		if models[i].ID != "openrouter/auto" {
			continue
		}
		models[i].Aliases = mergeOpenRouterAliases(models[i].Aliases, aliases)
		return models
	}
	return append(llm.Models{{
		ID:       "openrouter/auto",
		Name:     "OpenRouter Auto",
		Provider: providerName,
		Aliases:  aliases,
	}}, models...)
}

func mergeOpenRouterAliases(existing, extra []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(extra))
	out := make([]string, 0, len(existing)+len(extra))
	for _, values := range [][]string{existing, extra} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func selectAPI(model string, hint llm.ApiType) (orAPIBackend, llm.ApiType) {
	if hint == llm.ApiTypeAnthropicMessages {
		return orMessages, llm.ApiTypeAnthropicMessages
	}
	if strings.HasPrefix(model, "anthropic/") {
		return orMessages, llm.ApiTypeAnthropicMessages
	}
	return orResponses, llm.ApiTypeOpenAIResponses
}

func upstreamProviderFromModel(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 {
		return model[:i]
	}
	return providerName
}

func (p *Provider) normalizeRequestModel(model string) string {
	switch model {
	case "", llm.ModelDefault:
		return p.defaultModel
	default:
		return model
	}
}

// stripThinkingParts removes thinking parts from a message since responses API doesn't support them
func stripThinkingParts(message msg.Message) msg.Message {
	var filteredParts []msg.Part
	for _, part := range message.Parts {
		if part.Type == msg.PartTypeThinking {
			continue // skip thinking parts
		}
		filteredParts = append(filteredParts, part)
	}
	message.Parts = filteredParts
	return message
}

type orAPIBackend int

const (
	orResponses orAPIBackend = iota
	orMessages
)
