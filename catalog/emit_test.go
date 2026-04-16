package catalog

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadJSONRoundTrip(t *testing.T) {
	timepoint := time.Unix(123, 0).UTC()
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	c := NewCatalog()
	c.Services["anthropic"] = Service{ID: "anthropic", Name: "Anthropic", Provenance: []Provenance{{SourceID: "test", ObservedAt: timepoint}}}
	c.Models[key] = ModelRecord{Key: key, Name: "Claude Sonnet 4.6", Canonical: true, Provenance: []Provenance{{SourceID: "test", ObservedAt: timepoint}}}
	c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-6"}] = Offering{
		ServiceID:   "anthropic",
		WireModelID: "claude-sonnet-4-6",
		ModelKey:    key,
		Provenance:  []Provenance{{SourceID: "test", ObservedAt: timepoint}},
	}

	path := filepath.Join(t.TempDir(), "catalog.json")
	require.NoError(t, SaveJSON(path, c))

	loaded, err := LoadJSON(path)
	require.NoError(t, err)
	assert.Equal(t, c, loaded)
	assert.NoError(t, ValidateCatalog(loaded))
}
