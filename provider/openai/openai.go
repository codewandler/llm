package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/codewandler/llm"
	providercore2 "github.com/codewandler/llm/internal/providercore"
)

const (
	defaultBaseURL = "https://api.openai.com"
	providerName   = "openai"

	DefaultModel = "gpt-4o-mini"
)

type Provider struct {
	inner *providercore2.Provider
	opts  *llm.Options
}

func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("OPENAI_API_KEY", "OPENAI_KEY"),
	}
}

func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)

	inner := providercore2.NewProvider(providercore2.NewOptions(
		providercore2.WithProviderName(providerName),
		providercore2.WithBaseURL(defaultBaseURL),
		providercore2.WithAPIHint(llm.ApiTypeOpenAIChatCompletion),
		providercore2.WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			return loadOpenAIModels(providerName), nil
		}),
		providercore2.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			key, err := cfg.ResolveAPIKey(ctx)
			if err != nil || key == "" {
				return nil, llm.NewErrMissingAPIKey(providerName)
			}
			return http.Header{"Authorization": {"Bearer " + key}}, nil
		}),
		providercore2.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			mapped, err := mapEffortAndThinking(req.Model, req.Effort, req.Thinking)
			if err != nil {
				return req, original, err
			}
			req.Effort = llm.Effort(mapped)
			return req, original, nil
		}),
		providercore2.WithAPIHintResolver(func(req llm.Request) llm.ApiType {
			if useResponsesAPI(req.Model) {
				return llm.ApiTypeOpenAIResponses
			}
			return llm.ApiTypeOpenAIChatCompletion
		}),
		providercore2.WithHTTPErrorActionResolver(func(_ llm.Request, statusCode int, _ error) providercore2.HTTPErrorAction {
			if llm.IsRetriableHTTPStatus(statusCode) {
				return providercore2.HTTPErrorActionReturn
			}
			return providercore2.HTTPErrorActionStream
		}),
	), allOpts...)

	return &Provider{inner: inner, opts: cfg}
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.inner.CreateStream(ctx, src)
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("get API key: %w", err)
	}

	client := p.opts.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]llm.Model, 0, len(result.Data))
	for _, m := range result.Data {
		models = append(models, llm.Model{
			ID:       m.ID,
			Name:     m.ID,
			Provider: p.Name(),
		})
	}
	return models, nil
}
