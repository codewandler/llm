package auto

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithClaudeAccountAddsDetectedProvider(t *testing.T) {
	cfg := &config{}
	store := newMockTokenStore()
	WithClaudeAccount("work", store)(cfg)
	require.Len(t, cfg.detectedProviders, 1)
	assert.Equal(t, ProviderClaude, cfg.detectedProviders[0].Type)
	assert.Equal(t, "work", cfg.detectedProviders[0].Name)
}

func TestNewReturnsService(t *testing.T) {
	ctx := context.Background()
	svc, err := New(ctx, WithoutAutoDetect(), WithOpenAI())
	require.NoError(t, err)
	require.NotNil(t, svc)
}
