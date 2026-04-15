package apicore_test

import (
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
)

func TestAdapterConfig_Defaults(t *testing.T) {
	cfg := apicore.ApplyAdapterOptions()
	assert.Equal(t, "unknown", cfg.Provider())
	assert.Equal(t, "unknown", cfg.Upstream())
}

func TestAdapterConfig_WithProviderName(t *testing.T) {
	cfg := apicore.ApplyAdapterOptions(apicore.WithProviderName("openai"))
	assert.Equal(t, "openai", cfg.Provider())
	assert.Equal(t, "openai", cfg.Upstream())
}

func TestAdapterConfig_WithUpstreamProvider(t *testing.T) {
	cfg := apicore.ApplyAdapterOptions(
		apicore.WithProviderName("router"),
		apicore.WithUpstreamProvider("anthropic"),
	)
	assert.Equal(t, "router", cfg.Provider())
	assert.Equal(t, "anthropic", cfg.Upstream())
}
