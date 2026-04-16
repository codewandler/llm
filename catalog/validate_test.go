package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateCatalogMissingService(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	c := NewCatalog()
	c.Models[key] = ModelRecord{Key: key}
	c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"}] = Offering{
		ServiceID:   "anthropic",
		WireModelID: "claude-sonnet-4-6",
		ModelKey:    key,
	}

	err := ValidateCatalog(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown service")
}

func TestValidateCatalogMissingModel(t *testing.T) {
	c := NewCatalog()
	c.Services["anthropic"] = Service{ID: "anthropic"}
	c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"}] = Offering{
		ServiceID:   "anthropic",
		WireModelID: "claude-sonnet-4-6",
		ModelKey:    NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"}),
	}

	err := ValidateCatalog(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown model")
}

func TestValidateResolvedCatalogMissingRuntimeOffering(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	base := NewCatalog()
	base.Services["anthropic"] = Service{ID: "anthropic"}
	base.Models[key] = ModelRecord{Key: key}
	resolved := NewResolvedCatalog(base)
	resolved.Runtimes["local"] = Runtime{ID: "local", ServiceID: "anthropic"}
	resolved.RuntimeAccess[RuntimeAccessKey{RuntimeID: "local", ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"}] = RuntimeAccess{
		RuntimeID: "local",
		Offering:  OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"},
		Routable:  true,
	}

	err := ValidateResolvedCatalog(resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown offering")
}
