package catalog

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilder_RebindsOpenRouterOfferingToCanonicalAnthropicRelease(t *testing.T) {
	built, err := Builder{Sources: []RegisteredSource{
		{Stage: StageBuild, Authority: AuthorityCanonical, Source: NewAnthropicStaticSource()},
		{
			Stage:     StageBuild,
			Authority: AuthorityTrusted,
			Source: SourceFunc{
				SourceID: "openrouter-api",
				FetchFunc: func(context.Context) (*Fragment, error) {
					lineKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5"})
					return &Fragment{
						Services: []Service{{ID: "openrouter", Name: "OpenRouter", Kind: ServiceKindBroker}},
						Models: []ModelRecord{{
							Key:       lineKey,
							Name:      "Anthropic: Claude Sonnet 4.5",
							Canonical: false,
							Provenance: []Provenance{{
								SourceID:  "openrouter-api",
								Authority: string(AuthorityTrusted),
								RawID:     "anthropic/claude-sonnet-4.5",
							}},
						}},
						Offerings: []Offering{{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4.5", ModelKey: lineKey}},
					}, nil
				},
			},
		},
	}}.Build(context.Background())
	require.NoError(t, err)

	line := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5"})
	release := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5", ReleaseDate: "2025-09-29"})
	assertModelKeysForLine(t, built, line, []ModelKey{release})

	offering, ok := built.Offerings[OfferingRef{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4.5"}]
	require.True(t, ok)
	assert.Equal(t, release, offering.ModelKey)
	model, ok := built.Models[release]
	require.True(t, ok)
	assert.Equal(t, "Claude Sonnet 4.5", model.Name)
	assert.True(t, len(model.Provenance) >= 2)
}

func TestBuilder_PrefersLatestReleasedCreatorRootForLineRebinding(t *testing.T) {
	lineKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5"})
	releaseEarly := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5", ReleaseDate: "2025-08-01"})
	releaseLate := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5", ReleaseDate: "2025-09-29"})

	built, err := Builder{Sources: []RegisteredSource{
		creatorSource("creator-one", &Fragment{Models: []ModelRecord{{Key: lineKey, Name: "Line Root", Canonical: true}}}),
		creatorSource("creator-two", &Fragment{Models: []ModelRecord{{Key: releaseEarly, Name: "Release Early", Canonical: true}, {Key: releaseLate, Name: "Release Late", Canonical: true}}}),
		enrichmentSource("openrouter-api", &Fragment{Models: []ModelRecord{{Key: lineKey, Name: "Line Enrichment"}}, Offerings: []Offering{{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4.5", ModelKey: lineKey}}, Services: []Service{{ID: "openrouter", Name: "OpenRouter", Kind: ServiceKindBroker}}}),
	}}.Build(context.Background())
	require.NoError(t, err)

	offering, ok := built.Offerings[OfferingRef{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4.5"}]
	require.True(t, ok)
	assert.Equal(t, releaseLate, offering.ModelKey)
	assertModelKeysForLine(t, built, lineKey, []ModelKey{lineKey, releaseEarly, releaseLate})
}

func TestBuilder_KeepsProvisionalRootWhenCreatorMissing(t *testing.T) {
	lineKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.5"})
	built, err := Builder{Sources: []RegisteredSource{
		enrichmentSource("openrouter-api", &Fragment{
			Services:  []Service{{ID: "openrouter", Name: "OpenRouter", Kind: ServiceKindBroker}},
			Models:    []ModelRecord{{Key: lineKey, Name: "Anthropic: Claude Sonnet 4.5", Canonical: false}},
			Offerings: []Offering{{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4.5", ModelKey: lineKey}},
		}),
	}}.Build(context.Background())
	require.NoError(t, err)

	model, ok := built.Models[lineKey]
	require.True(t, ok)
	assert.False(t, model.Canonical)
	offering, ok := built.Offerings[OfferingRef{ServiceID: "openrouter", WireModelID: "anthropic/claude-sonnet-4.5"}]
	require.True(t, ok)
	assert.Equal(t, lineKey, offering.ModelKey)
}

func creatorSource(id string, frag *Fragment) RegisteredSource {
	return RegisteredSource{
		Stage:     StageBuild,
		Authority: AuthorityCanonical,
		Source: SourceFunc{SourceID: id, FetchFunc: func(context.Context) (*Fragment, error) {
			return frag, nil
		}},
	}
}

func enrichmentSource(id string, frag *Fragment) RegisteredSource {
	return RegisteredSource{
		Stage:     StageBuild,
		Authority: AuthorityTrusted,
		Source: SourceFunc{SourceID: id, FetchFunc: func(context.Context) (*Fragment, error) {
			return frag, nil
		}},
	}
}

func assertModelKeysForLine(t *testing.T, c Catalog, line ModelKey, want []ModelKey) {
	t.Helper()
	var got []ModelKey
	for key := range c.Models {
		if LineKey(key) == LineKey(line) {
			got = append(got, NormalizeKey(key))
		}
	}
	sortModelKeys(got)
	sortModelKeys(want)
	assert.Equal(t, want, got)
}

func sortModelKeys(keys []ModelKey) {
	if len(keys) < 2 {
		return
	}
	sort.Slice(keys, func(i, j int) bool {
		return modelID(keys[i]) < modelID(keys[j])
	})
}
