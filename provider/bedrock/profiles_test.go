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
		{"us-east-1", "us"},
		{"us-west-2", "us"},
		{"us-gov-west-1", "us"},

		// EU regions
		{"eu-central-1", "eu"},
		{"eu-west-1", "eu"},
		{"eu-north-1", "eu"},

		// Asia-Pacific regions
		{"ap-northeast-1", "apac"},
		{"ap-southeast-1", "apac"},
		{"ap-south-1", "apac"},

		// Unknown regions default to global
		{"sa-east-1", "global"},
		{"me-south-1", "global"},
		{"unknown-region", "global"},
		{"", "global"},
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
		{"us.anthropic.claude-sonnet-4-6", true},
		{"eu.anthropic.claude-sonnet-4-6", true},
		{"apac.anthropic.claude-sonnet-4-6", true},
		{"global.anthropic.claude-sonnet-4-6", true},

		// Without prefix
		{"anthropic.claude-sonnet-4-6", false},
		{"meta.llama3-70b-instruct-v1:0", false},
		{"amazon.nova-pro-v1:0", false},

		// Edge cases
		{"", false},
		{"us", false}, // Too short
		{"eu", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := hasRegionPrefix(tt.model)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsPrefix(t *testing.T) {
	prefixes := []string{"eu", "us", "global"}

	assert.True(t, containsPrefix(prefixes, "eu"))
	assert.True(t, containsPrefix(prefixes, "us"))
	assert.True(t, containsPrefix(prefixes, "global"))
	assert.False(t, containsPrefix(prefixes, "apac"))
	assert.False(t, containsPrefix(prefixes, ""))
	assert.False(t, containsPrefix(nil, "us"))
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
			region:   "eu-central-1",
			model:    "us.anthropic.claude-sonnet-4-6",
			expected: "us.anthropic.claude-sonnet-4-6",
		},
		{
			name:     "passthrough eu prefix",
			region:   "us-east-1",
			model:    "eu.anthropic.claude-sonnet-4-6",
			expected: "eu.anthropic.claude-sonnet-4-6",
		},

		// Regional prefix applied
		{
			name:     "us region applies us prefix",
			region:   "us-east-1",
			model:    "anthropic.claude-sonnet-4-6",
			expected: "us.anthropic.claude-sonnet-4-6",
		},
		{
			name:     "eu region applies eu prefix",
			region:   "eu-central-1",
			model:    "anthropic.claude-sonnet-4-6",
			expected: "eu.anthropic.claude-sonnet-4-6",
		},

		// Fallback to global when regional not available
		{
			name:     "apac region falls back to global for claude-sonnet-4-6",
			region:   "ap-northeast-1",
			model:    "anthropic.claude-sonnet-4-6",
			expected: "global.anthropic.claude-sonnet-4-6",
		},

		// No profile - passthrough unchanged
		{
			name:     "no profile - passthrough",
			region:   "us-east-1",
			model:    "meta.llama3-70b-instruct-v1:0",
			expected: "meta.llama3-70b-instruct-v1:0",
		},

		// Error - model not available in region and no global fallback
		{
			name:        "error - us-only model in eu region",
			region:      "eu-central-1",
			model:       "meta.llama4-maverick-17b-instruct-v1:0", // Only available in us
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
		{"us-east-1", "us"},
		{"eu-central-1", "eu"},
		{"ap-northeast-1", "apac"},
		{"sa-east-1", "global"},
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
		"anthropic.claude-sonnet-4-6",
		"anthropic.claude-opus-4-6-v1",
		"anthropic.claude-haiku-4-5-20251001-v1:0",
		"amazon.nova-pro-v1:0",
	}

	for _, model := range keyModels {
		t.Run(model, func(t *testing.T) {
			profile, ok := inferenceProfiles[model]
			require.True(t, ok, "model %q should be in inference profiles registry", model)
			require.NotEmpty(t, profile.Prefixes, "model %q should have prefixes", model)
		})
	}
}
