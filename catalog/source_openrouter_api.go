package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

const defaultOpenRouterBaseURL = "https://openrouter.ai/api"

type OpenRouterSource struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

func NewOpenRouterSource(apiKey string) OpenRouterSource {
	return OpenRouterSource{APIKey: apiKey, BaseURL: defaultOpenRouterBaseURL, Client: http.DefaultClient}
}

func NewOpenRouterSourceFromEnv() OpenRouterSource {
	return NewOpenRouterSource(os.Getenv("OPENROUTER_API_KEY"))
}

func (OpenRouterSource) ID() string { return "openrouter-api" }

func (s OpenRouterSource) Fetch(ctx context.Context) (*Fragment, error) {
	if s.APIKey == "" {
		return nil, fmt.Errorf("openrouter source: missing API key")
	}
	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenRouterBaseURL
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter source: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			CanonicalSlug string `json:"canonical_slug"`
			Name          string `json:"name"`
			Architecture  struct {
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
			Pricing struct {
				Prompt         string `json:"prompt"`
				Completion     string `json:"completion"`
				InputCacheRead string `json:"input_cache_read"`
			} `json:"pricing"`
			TopProvider struct {
				ContextLength       int `json:"context_length"`
				MaxCompletionTokens int `json:"max_completion_tokens"`
			} `json:"top_provider"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	observedAt := time.Now().UTC()
	fragment := &Fragment{Services: []Service{{
		ID:       "openrouter",
		Name:     "OpenRouter",
		Kind:     ServiceKindBroker,
		Operator: "openrouter",
		Provenance: []Provenance{{
			SourceID:   s.ID(),
			Authority:  string(AuthorityTrusted),
			ObservedAt: observedAt,
		}},
	}}}

	for _, item := range payload.Data {
		key, ok := inferOpenRouterModelKey(item.ID)
		if !ok {
			continue
		}
		fragment.Models = append(fragment.Models, ModelRecord{
			Key:              key,
			Canonical:        false,
			Capabilities:     capabilitiesFromOpenRouter(item.SupportedParameters),
			InputModalities:  normalizeStrings(item.Architecture.InputModalities),
			OutputModalities: normalizeStrings(item.Architecture.OutputModalities),
			Provenance: []Provenance{{
				SourceID:   s.ID(),
				Authority:  string(AuthorityTrusted),
				ObservedAt: observedAt,
				RawID:      item.ID,
			}},
		})
		offering := Offering{
			ServiceID:   "openrouter",
			WireModelID: item.ID,
			ModelKey:    key,
			Pricing: pricingFromOpenRouter(
				item.Pricing.Prompt,
				item.Pricing.Completion,
				item.Pricing.InputCacheRead,
			),
			LimitsOverride: limitsPtr(item.TopProvider.ContextLength, item.TopProvider.MaxCompletionTokens),
			Provenance: []Provenance{{
				SourceID:   s.ID(),
				Authority:  string(AuthorityTrusted),
				ObservedAt: observedAt,
				RawID:      item.ID,
			}},
		}
		if item.CanonicalSlug != "" && item.CanonicalSlug != item.ID {
			offering.Aliases = []string{item.CanonicalSlug}
		}
		fragment.Offerings = append(fragment.Offerings, offering)
	}

	return fragment, nil
}

func capabilitiesFromOpenRouter(params []string) Capabilities {
	return Capabilities{
		ToolUse:          containsString(params, "tools") || containsString(params, "tool_choice"),
		StructuredOutput: containsString(params, "response_format"),
		Temperature:      containsString(params, "temperature"),
	}
}

func pricingFromOpenRouter(prompt, completion, cacheRead string) *Pricing {
	p := &Pricing{}
	if v, ok := parsePerTokenDollars(prompt); ok {
		p.Input = v
	}
	if v, ok := parsePerTokenDollars(completion); ok {
		p.Output = v
	}
	if v, ok := parsePerTokenDollars(cacheRead); ok {
		p.CachedInput = v
	}
	if p.Input == 0 && p.Output == 0 && p.CachedInput == 0 {
		return nil
	}
	return p
}

func parsePerTokenDollars(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return v * 1_000_000, true
}
