package catalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterSourceFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic/claude-sonnet-4-6","canonical_slug":"claude-sonnet-4-6","name":"Claude Sonnet 4.6","architecture":{"modality":"text->text","input_modalities":["text","image"],"output_modalities":["text"],"instruct_type":"claude","tokenizer":"Claude"},"pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000003"},"top_provider":{"context_length":200000,"max_completion_tokens":32000,"is_moderated":true},"supported_parameters":["tools","tool_choice","temperature","response_format","reasoning","logprobs"],"description":"Claude Sonnet 4.6 model"}]}`))
	}))
	defer server.Close()

	source := NewOpenRouterSource("test-key")
	source.BaseURL = server.URL
	source.Client = server.Client()

	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)
	require.Len(t, fragment.Services, 1)
	require.Len(t, fragment.Offerings, 1)
	require.Len(t, fragment.Models, 1)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	model, ok := c.Models[key]
	require.True(t, ok)
	assert.Equal(t, "Claude Sonnet 4.6", model.Name)
	assert.Equal(t, "Claude Sonnet 4.6 model", model.Description)
	assert.Equal(t, "text->text", model.Modality)
	assert.Equal(t, "claude", model.InstructType)
	assert.Equal(t, "Claude", model.Tokenizer)
	assert.True(t, model.Capabilities.ToolUse)
	assert.True(t, model.Capabilities.StructuredOutput)
	assert.True(t, model.Capabilities.Reasoning)
	assert.True(t, model.Capabilities.Logprobs)
	assert.True(t, model.Capabilities.Vision)

	offering, ok := c.Offerings[OfferingRef{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4-6"}]
	require.True(t, ok)
	assert.Equal(t, key, offering.ModelKey)
	assert.Equal(t, []string{"claude-sonnet-4-6"}, offering.Aliases)
	assert.NotNil(t, offering.Pricing)
	assert.Equal(t, 3.0, offering.Pricing.Input)
	assert.Equal(t, 15.0, offering.Pricing.Output)
	assert.Equal(t, 0.3, offering.Pricing.CachedInput)
	assert.True(t, offering.IsModerated)
	assert.Contains(t, offering.SupportedParameters, "tools")
	assert.Contains(t, offering.SupportedParameters, "reasoning")
}

func TestOpenRouterSourceFetch_LogUnhandledModels(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("requires OPENROUTER_API_KEY")
	}

	source := NewOpenRouterSourceFromEnv()
	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)

	type modelEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	resp, err := http.Get("https://openrouter.ai/api/v1/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var payload struct {
		Data []modelEntry `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))

	handledIDs := make(map[string]bool, len(fragment.Models)+len(fragment.Offerings))
	for _, o := range fragment.Offerings {
		handledIDs[o.WireModelID] = true
	}

	var unhandled []modelEntry
	for _, m := range payload.Data {
		if !handledIDs[m.ID] {
			unhandled = append(unhandled, m)
		}
	}

	sort.Slice(unhandled, func(i, j int) bool {
		return unhandled[i].ID < unhandled[j].ID
	})

	t.Logf("Total models from API: %d", len(payload.Data))
	t.Logf("Handled by inferOpenRouterModelKey: %d", len(handledIDs))
	t.Logf("Unhandled models: %d", len(unhandled))
	for _, m := range unhandled {
		t.Logf("  UNHANDLED: %-55s %s", m.ID, m.Name)
	}
}
