package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelDBSourceFetch(t *testing.T) {
	fragment, err := NewModelDBSource().Fetch(context.Background())
	require.NoError(t, err)
	require.NotNil(t, fragment)
	require.NotEmpty(t, fragment.Services)
	require.NotEmpty(t, fragment.Offerings)

	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, fragment))
	require.NoError(t, ValidateCatalog(c))

	_, ok := c.Services["openai"]
	assert.True(t, ok)
	_, ok = c.Services["anthropic"]
	assert.True(t, ok)

	require.NotEmpty(t, fragment.Offerings)
	offering := fragment.Offerings[0]
	_, hasOffering := c.Offerings[OfferingRef{ServiceID: offering.ServiceID, WireModelID: offering.WireModelID}]
	assert.True(t, hasOffering)
	_, hasModel := c.Models[offering.ModelKey]
	assert.True(t, hasModel)
}
