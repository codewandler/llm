package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

type OpenAISource struct {
	APIKey  string
	BaseURL string
	Client  *http.Client
}

func NewOpenAISource(apiKey string) OpenAISource {
	return OpenAISource{APIKey: apiKey, BaseURL: defaultOpenAIBaseURL, Client: http.DefaultClient}
}

func NewOpenAISourceFromEnv() OpenAISource {
	return NewOpenAISource(firstNonEmpty(os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_KEY")))
}

func (OpenAISource) ID() string { return "openai-api" }

func (s OpenAISource) Fetch(ctx context.Context) (*Fragment, error) {
	if s.APIKey == "" {
		return nil, fmt.Errorf("openai source: missing API key")
	}
	baseURL := s.BaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
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
		return nil, fmt.Errorf("openai source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai source: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	observedAt := time.Now().UTC()
	fragment := &Fragment{Services: []Service{{
		ID:       "openai",
		Name:     "OpenAI",
		Kind:     ServiceKindDirect,
		Operator: "openai",
		Provenance: []Provenance{{
			SourceID:   s.ID(),
			Authority:  string(AuthorityTrusted),
			ObservedAt: observedAt,
		}},
	}}}

	for _, item := range payload.Data {
		key, ok := inferOpenAIModelKey(item.ID)
		if !ok {
			continue
		}
		fragment.Models = append(fragment.Models, ModelRecord{
			Key:       key,
			Canonical: false,
			Provenance: []Provenance{{
				SourceID:   s.ID(),
				Authority:  string(AuthorityTrusted),
				ObservedAt: observedAt,
				RawID:      item.ID,
			}},
		})
		fragment.Offerings = append(fragment.Offerings, Offering{
			ServiceID:   "openai",
			WireModelID: item.ID,
			ModelKey:    key,
			Provenance: []Provenance{{
				SourceID:   s.ID(),
				Authority:  string(AuthorityTrusted),
				ObservedAt: observedAt,
				RawID:      item.ID,
			}},
		})
	}

	return fragment, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
