package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicStaticSourceFetch(t *testing.T) {
	source := NewAnthropicStaticSource()
	fragment, err := source.Fetch(context.Background())
	require.NoError(t, err)
	require.NotNil(t, fragment)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	service, ok := c.Services["anthropic"]
	require.True(t, ok)
	assert.Equal(t, "Anthropic", service.Name)

	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	model, ok := c.Models[key]
	require.True(t, ok)
	assert.True(t, model.Canonical)
	assert.Contains(t, model.Aliases, "claude-sonnet-4-6")

	offering, ok := c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"}]
	require.True(t, ok)
	assert.Equal(t, key, offering.ModelKey)
	assert.Contains(t, offering.Aliases, "default")
	assert.Contains(t, offering.Aliases, "fast")
	assert.Equal(t, []string{"anthropic-messages"}, offering.APITypes)
	assert.NotNil(t, offering.Pricing)

	releaseKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "haiku", Version: "4.5", ReleaseDate: "2025-10-01"})
	_, ok = c.Models[releaseKey]
	require.True(t, ok)
	assert.Equal(t, "anthropic/claude/haiku/4.5@2025-10-01", ReleaseID(releaseKey))
}
