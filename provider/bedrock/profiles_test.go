package bedrock

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeRegionPrefix(t *testing.T) {
	tests := []struct {
		region   string
		expected string
	}{
		// US regions
		{RegionUSEast1, PrefixUS},
		{RegionUSWest2, PrefixUS},
		{"us-gov-west-1", PrefixUS},

		// EU regions
		{RegionEUCentral1, PrefixEU},
		{RegionEUWest1, PrefixEU},
		{"eu-north-1", PrefixEU},

		// Asia-Pacific regions
		{RegionAPNortheast1, PrefixAPAC},
		{"ap-southeast-1", PrefixAPAC},
		{"ap-south-1", PrefixAPAC},

		// Unknown regions default to global
		{"sa-east-1", PrefixGlobal},
		{"me-south-1", PrefixGlobal},
		{"unknown-region", PrefixGlobal},
		{"", PrefixGlobal},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			result := computeRegionPrefix(tt.region)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasRegionPrefix(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		// With prefix
		{PrefixUS + "." + ModelSonnetLatest, true},
		{PrefixEU + "." + ModelSonnetLatest, true},
		{PrefixAPAC + "." + ModelSonnetLatest, true},
		{PrefixGlobal + "." + ModelSonnetLatest, true},

		// Without prefix
		{ModelSonnetLatest, false},
		{ModelLlama3_70B, false},
		{ModelNovaPro, false},

		// Edge cases
		{"", false},
		{PrefixUS, false}, // Too short
		{PrefixEU, false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := hasRegionPrefix(tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsPrefix(t *testing.T) {
	prefixes := []string{PrefixEU, PrefixUS, PrefixGlobal}

	assert.True(t, containsPrefix(prefixes, PrefixEU))
	assert.True(t, containsPrefix(prefixes, PrefixUS))
	assert.True(t, containsPrefix(prefixes, PrefixGlobal))
	assert.False(t, containsPrefix(prefixes, PrefixAPAC))
	assert.False(t, containsPrefix(prefixes, ""))
	assert.False(t, containsPrefix(nil, PrefixUS))
}

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name        string
		region      string
		model       string
		expected    string
		expectError bool
	}{
		// Passthrough - already has prefix
		{
			name:     "passthrough us prefix",
			region:   RegionEUCentral1,
			model:    PrefixUS + "." + ModelSonnetLatest,
			expected: PrefixUS + "." + ModelSonnetLatest,
		},
		{
			name:     "passthrough eu prefix",
			region:   RegionUSEast1,
			model:    PrefixEU + "." + ModelSonnetLatest,
			expected: PrefixEU + "." + ModelSonnetLatest,
		},

		// Regional prefix applied
		{
			name:     "us region applies us prefix",
			region:   RegionUSEast1,
			model:    ModelSonnetLatest,
			expected: PrefixUS + "." + ModelSonnetLatest,
		},
		{
			name:     "eu region applies eu prefix",
			region:   RegionEUCentral1,
			model:    ModelSonnetLatest,
			expected: PrefixEU + "." + ModelSonnetLatest,
		},

		// Fallback to global when regional not available
		{
			name:     "apac region falls back to global for ModelSonnetLatest",
			region:   RegionAPNortheast1,
			model:    ModelSonnetLatest,
			expected: PrefixGlobal + "." + ModelSonnetLatest,
		},

		// No profile - passthrough unchanged
		{
			name:     "no profile - passthrough",
			region:   RegionUSEast1,
			model:    ModelLlama3_70B,
			expected: ModelLlama3_70B,
		},

		// Error - model not available in region and no global fallback
		{
			name:        "error - us-only model in eu region",
			region:      RegionEUCentral1,
			model:       ModelLlama4Maverick, // Only available in us
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(WithRegion(tt.region))

			result, err := p.resolveModel(tt.model)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not available in region")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestRegionPrefix(t *testing.T) {
	tests := []struct {
		region   string
		expected string
	}{
		{RegionUSEast1, PrefixUS},
		{RegionEUCentral1, PrefixEU},
		{RegionAPNortheast1, PrefixAPAC},
		{"sa-east-1", PrefixGlobal},
	}

	for _, tt := range tests {
		t.Run(tt.region, func(t *testing.T) {
			p := New(WithRegion(tt.region))
			assert.Equal(t, tt.expected, p.RegionPrefix())
		})
	}
}

func TestInferenceProfilesRegistry(t *testing.T) {
	// Verify some key models are in the registry
	keyModels := []string{
		ModelSonnetLatest,
		ModelOpusLatest,
		ModelHaikuLatest,
		ModelNovaPro,
	}

	for _, model := range keyModels {
		t.Run(model, func(t *testing.T) {
			profile, ok := inferenceProfiles[model]
			require.True(t, ok, "model %q should be in inference profiles registry", model)
			require.NotEmpty(t, profile.Prefixes, "model %q should have prefixes", model)
		})
	}
}
