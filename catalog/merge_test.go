package catalog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeCatalogFragmentFillsEmptyFields(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	c := NewCatalog()

	err := MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{Key: key, Name: "Claude Sonnet 4.6", Canonical: true}}})
	require.NoError(t, err)
	err = MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{Key: key, Aliases: []string{"sonnet"}, Limits: Limits{ContextWindow: 200000}}}})
	require.NoError(t, err)

	model := c.Models[key]
	assert.Equal(t, "Claude Sonnet 4.6", model.Name)
	assert.Equal(t, []string{"sonnet"}, model.Aliases)
	assert.Equal(t, 200000, model.Limits.ContextWindow)
	assert.True(t, model.Canonical)
}

func TestMergeCatalogFragmentRejectsConflictingScalar(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{Key: key, Name: "Claude Sonnet 4.6"}}}))

	err := MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{Key: key, Name: "Different Name"}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model.name conflict")
}

func TestMergeCatalogFragmentUnionsAliases(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"})
	c := NewCatalog()
	require.NoError(t, MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{Key: key, Aliases: []string{"opus", "powerful"}}}}))
	require.NoError(t, MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{Key: key, Aliases: []string{"opus", "flagship"}}}}))

	assert.Equal(t, []string{"opus", "powerful", "flagship"}, c.Models[key].Aliases)
}

func TestMergeCatalogFragmentRejectsConflictingOfferingMapping(t *testing.T) {
	keyA := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	keyB := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "opus", Version: "4.6"})
	c := NewCatalog()

	require.NoError(t, MergeCatalogFragment(&c, &Fragment{
		Services: []Service{{ID: "anthropic"}},
		Models:   []ModelRecord{{Key: keyA}, {Key: keyB}},
		Offerings: []Offering{{
			ServiceID:   "anthropic",
			WireModelID: "claude-sonnet-4-6",
			ModelKey:    keyA,
		}},
	}))

	err := MergeCatalogFragment(&c, &Fragment{Offerings: []Offering{{
		ServiceID:   "anthropic",
		WireModelID: "claude-sonnet-4-6",
		ModelKey:    keyB,
	}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "maps to conflicting model keys")
}

func TestMergeCatalogFragmentAppendsProvenance(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	c := NewCatalog()
	t1 := time.Unix(100, 0).UTC()
	t2 := time.Unix(200, 0).UTC()

	require.NoError(t, MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{
		Key:        key,
		Provenance: []Provenance{{SourceID: "a", ObservedAt: t1}},
	}}}))
	require.NoError(t, MergeCatalogFragment(&c, &Fragment{Models: []ModelRecord{{
		Key:        key,
		Provenance: []Provenance{{SourceID: "b", ObservedAt: t2}},
	}}}))

	assert.Len(t, c.Models[key].Provenance, 2)
}
