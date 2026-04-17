package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectOfferingsByModel_BuiltInSonnet45(t *testing.T) {
	c, err := LoadBuiltIn()
	require.NoError(t, err)

	selector, err := ParseModelSelector("sonnet", "4.5")
	require.NoError(t, err)

	selection, err := c.SelectOfferingsByModel(selector)
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude/sonnet/4.5@2025-09-29", formatModelID(selection.Model.Key))

	byService := make(map[string]string, len(selection.Offerings))
	for _, item := range selection.Offerings {
		byService[item.Service.ID] = item.Offering.WireModelID
	}
	assert.Len(t, byService, len(selection.Offerings), "expected one offering per service")
	assert.Equal(t, "claude-sonnet-4-5-20250929", byService["anthropic"])
	assert.Equal(t, "anthropic.claude-sonnet-4-5-20250929-v1:0", byService["bedrock"])
	assert.Equal(t, "anthropic/claude-sonnet-4.5", byService["openrouter"])
	assert.GreaterOrEqual(t, len(selection.Offerings), 3)
}

func TestSelectModel_BuiltInSonnet45ResolvesCanonicalRelease(t *testing.T) {
	c, err := LoadBuiltIn()
	require.NoError(t, err)

	model, err := c.SelectModel(ModelSelector{Name: "sonnet", Version: "4.5"})
	require.NoError(t, err)
	assert.Equal(t, "anthropic/claude/sonnet/4.5@2025-09-29", formatModelID(model.Key))
}

func TestSelectOfferingsByModel_PrefersUndatedWireIDPerService(t *testing.T) {
	key := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.0", ReleaseDate: "2025-05-14"})
	c := NewCatalog()
	c.Services["anthropic"] = Service{ID: "anthropic", Name: "Anthropic"}
	c.Models[key] = ModelRecord{Key: key, Name: "Claude Sonnet 4.0", Aliases: []string{"claude-sonnet-4", "claude-sonnet-4-20250514"}}
	c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4"}] = Offering{ServiceID: "anthropic", WireModelID: "claude-sonnet-4", ModelKey: key}
	c.Offerings[OfferingRef{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-20250514"}] = Offering{ServiceID: "anthropic", WireModelID: "claude-sonnet-4-20250514", ModelKey: key}

	selection, err := c.SelectOfferingsByModel(ModelSelector{Name: "sonnet", Version: "4.0"})
	require.NoError(t, err)
	require.Len(t, selection.Offerings, 1)
	assert.Equal(t, "claude-sonnet-4", selection.Offerings[0].Offering.WireModelID)
}

func TestSelectModel_AmbiguousSelectorListsCandidates(t *testing.T) {
	lineKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6"})
	releaseKey := NormalizeKey(ModelKey{Creator: "anthropic", Family: "claude", Series: "sonnet", Version: "4.6", ReleaseDate: "2026-02-17"})
	c := NewCatalog()
	c.Models[lineKey] = ModelRecord{Key: lineKey, Name: "Claude Sonnet 4.6", Aliases: []string{"sonnet"}}
	c.Models[releaseKey] = ModelRecord{Key: releaseKey, Name: "Claude Sonnet 4.6 (2026-02-17)", Aliases: []string{"sonnet"}}

	_, err := c.SelectModel(ModelSelector{Name: "sonnet", Version: "4.6"})
	require.Error(t, err)

	var ambiguous *AmbiguousModelSelectorError
	require.ErrorAs(t, err, &ambiguous)
	require.Len(t, ambiguous.Candidates, 2)
	assert.Equal(t, []string{
		"anthropic/claude/sonnet/4.6",
		"anthropic/claude/sonnet/4.6@2026-02-17",
	}, []string{formatModelID(ambiguous.Candidates[0].Key), formatModelID(ambiguous.Candidates[1].Key)})
}

func TestParseModelSelector_RequiresName(t *testing.T) {
	_, err := ParseModelSelector("", "4.5")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model name is required")
}
