package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferAnthropicModelKey(t *testing.T) {
	key, ok := inferAnthropicModelKey("claude-sonnet-4-5-20250929")
	require.True(t, ok)
	assert.Equal(t, NormalizeKey(ModelKey{
		Creator:     "anthropic",
		Family:      "claude",
		Series:      "sonnet",
		Version:     "4.5",
		ReleaseDate: "2025-09-29",
	}), key)
}

func TestInferOpenAIModelKey(t *testing.T) {
	key, ok := inferOpenAIModelKey("gpt-5.4-mini")
	require.True(t, ok)
	assert.Equal(t, NormalizeKey(ModelKey{
		Creator: "openai",
		Family:  "gpt",
		Version: "5.4",
		Variant: "mini",
	}), key)
}

func TestInferOpenRouterModelKey(t *testing.T) {
	key, ok := inferOpenRouterModelKey("anthropic/claude-opus-4-6:free")
	require.True(t, ok)
	assert.Equal(t, NormalizeKey(ModelKey{
		Creator: "anthropic",
		Family:  "claude",
		Series:  "opus",
		Version: "4.6",
	}), key)
}

func TestInferGoogleModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "gemini-2.5-flash",
			input: "google/gemini-2.5-flash",
			expected: ModelKey{
				Creator: "google",
				Family:  "gemini",
				Series:  "flash",
				Version: "2.5",
			},
		},
		{
			name:  "gemini-2.5-pro-preview-05-06",
			input: "google/gemini-2.5-pro-preview-05-06",
			expected: ModelKey{
				Creator:     "google",
				Family:      "gemini",
				Series:      "pro",
				Version:     "2.5",
				Variant:     "preview",
				ReleaseDate: "2006-05-01",
			},
		},
		{
			name:  "gemma-3-27b-it",
			input: "google/gemma-3-27b-it",
			expected: ModelKey{
				Creator: "google",
				Family:  "gemma",
				Version: "3",
				Variant: "27b-it",
			},
		},
		{
			name:  "lyria-3-clip-preview",
			input: "google/lyria-3-clip-preview",
			expected: ModelKey{
				Creator: "google",
				Family:  "lyria",
				Version: "3",
				Variant: "clip-preview",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferGoogleModelKey(tt.input[len("google/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferMetaLlamaModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "llama-3.3-70b-instruct",
			input: "meta-llama/llama-3.3-70b-instruct",
			expected: ModelKey{
				Creator: "meta",
				Family:  "llama",
				Version: "3.3",
				Variant: "70b-instruct",
			},
		},
		{
			name:  "llama-4-maverick",
			input: "meta-llama/llama-4-maverick",
			expected: ModelKey{
				Creator: "meta",
				Family:  "llama",
				Series:  "maverick",
				Version: "4",
			},
		},
		{
			name:  "llama-guard-4-12b",
			input: "meta-llama/llama-guard-4-12b",
			expected: ModelKey{
				Creator: "meta",
				Family:  "llama",
				Series:  "guard",
				Version: "4",
				Variant: "12b",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferMetaLlamaModelKey(tt.input[len("meta-llama/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferMistralModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "mistral-large-2512",
			input: "mistralai/mistral-large-2512",
			expected: ModelKey{
				Creator:     "mistral",
				Family:      "mistral",
				Series:      "large",
				ReleaseDate: "2025-12-01",
			},
		},
		{
			name:  "mistral-small-3.1-24b-instruct",
			input: "mistralai/mistral-small-3.1-24b-instruct",
			expected: ModelKey{
				Creator: "mistral",
				Family:  "mistral",
				Series:  "small",
				Version: "3.1",
				Variant: "24b-instruct",
			},
		},
		{
			name:  "codestral-2508",
			input: "mistralai/codestral-2508",
			expected: ModelKey{
				Creator:     "mistral",
				Family:      "codestral",
				ReleaseDate: "2025-08-01",
			},
		},
		{
			name:  "mixtral-8x22b-instruct",
			input: "mistralai/mixtral-8x22b-instruct",
			expected: ModelKey{
				Creator: "mistral",
				Family:  "mixtral",
				Variant: "8x22b-instruct",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferMistralModelKey(tt.input[len("mistralai/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferDeepseekModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "deepseek-r1-0528",
			input: "deepseek/deepseek-r1-0528",
			expected: ModelKey{
				Creator:     "deepseek",
				Family:      "deepseek",
				Series:      "r",
				Version:     "1",
				ReleaseDate: "2025-05-28",
			},
		},
		{
			name:  "deepseek-chat-v3.1",
			input: "deepseek/deepseek-chat-v3.1",
			expected: ModelKey{
				Creator: "deepseek",
				Family:  "deepseek",
				Version: "3.1",
			},
		},
		{
			name:  "deepseek-v3.2-speciale",
			input: "deepseek/deepseek-v3.2-speciale",
			expected: ModelKey{
				Creator: "deepseek",
				Family:  "deepseek",
				Version: "3.2",
				Variant: "speciale",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferDeepseekModelKey(tt.input[len("deepseek/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferXAIModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "grok-3-mini",
			input: "x-ai/grok-3-mini",
			expected: ModelKey{
				Creator: "xai",
				Family:  "grok",
				Version: "3",
				Variant: "mini",
			},
		},
		{
			name:  "grok-code-fast-1",
			input: "x-ai/grok-code-fast-1",
			expected: ModelKey{
				Creator: "xai",
				Family:  "grok",
				Series:  "code",
				Variant: "fast-1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferXAIModelKey(tt.input[len("x-ai/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferQwenModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "qwen3-235b-a22b",
			input: "qwen/qwen3-235b-a22b",
			expected: ModelKey{
				Creator: "alibaba",
				Family:  "qwen",
				Version: "3",
				Variant: "235b-a22b",
			},
		},
		{
			name:  "qwq-32b",
			input: "qwen/qwq-32b",
			expected: ModelKey{
				Creator: "alibaba",
				Family:  "qwq",
				Variant: "32b",
			},
		},
		{
			name:  "qwen-vl-max",
			input: "qwen/qwen-vl-max",
			expected: ModelKey{
				Creator: "alibaba",
				Family:  "qwen",
				Series:  "vl",
				Variant: "max",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferQwenModelKey(tt.input[len("qwen/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferNvidiaModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "nemotron-3-nano-30b-a3b",
			input: "nvidia/nemotron-3-nano-30b-a3b",
			expected: ModelKey{
				Creator: "nvidia",
				Family:  "nemotron",
				Version: "3",
				Variant: "nano-30b-a3b",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferNvidiaModelKey(tt.input[len("nvidia/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferAmazonModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "nova-pro-v1",
			input: "amazon/nova-pro-v1",
			expected: ModelKey{
				Creator: "amazon",
				Family:  "nova",
				Series:  "pro",
				Variant: "v1",
			},
		},
		{
			name:  "nova-2-lite-v1",
			input: "amazon/nova-2-lite-v1",
			expected: ModelKey{
				Creator: "amazon",
				Family:  "nova",
				Version: "2",
				Series:  "lite",
				Variant: "v1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferAmazonModelKey(tt.input[len("amazon/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferZhipuModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "glm-5.1",
			input: "z-ai/glm-5.1",
			expected: ModelKey{
				Creator: "zhipu",
				Family:  "glm",
				Version: "5.1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferZhipuModelKey(tt.input[len("z-ai/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferBaiduModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "ernie-4.5-vl-28b-a3b",
			input: "baidu/ernie-4.5-vl-28b-a3b",
			expected: ModelKey{
				Creator: "baidu",
				Family:  "ernie",
				Series:  "vl",
				Version: "4.5",
				Variant: "28b-a3b",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferBaiduModelKey(tt.input[len("baidu/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferMinimaxModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "minimax-m2.5",
			input: "minimax/minimax-m2.5",
			expected: ModelKey{
				Creator: "minimax",
				Family:  "minimax",
				Series:  "m",
				Variant: "2.5",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferMinimaxModelKey(tt.input[len("minimax/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferCohereModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "command-r-plus-08-2024",
			input: "cohere/command-r-plus-08-2024",
			expected: ModelKey{
				Creator:     "cohere",
				Family:      "command",
				Series:      "r",
				Variant:     "plus",
				ReleaseDate: "2024-08-01",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferCohereModelKey(tt.input[len("cohere/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferMoonshotModelKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "kimi-k2-thinking",
			input: "moonshotai/kimi-k2-thinking",
			expected: ModelKey{
				Creator: "moonshot",
				Family:  "kimi",
				Version: "2",
				Variant: "thinking",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferMoonshotModelKey(tt.input[len("moonshotai/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferOpenRouterMetaModel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ModelKey
	}{
		{
			name:  "openrouter/auto",
			input: "openrouter/auto",
			expected: ModelKey{
				Creator: "openrouter",
				Family:  "auto",
			},
		},
		{
			name:  "openrouter/free",
			input: "openrouter/free",
			expected: ModelKey{
				Creator: "openrouter",
				Family:  "free",
			},
		},
		{
			name:  "openrouter/elephant-alpha",
			input: "openrouter/elephant-alpha",
			expected: ModelKey{
				Creator: "openrouter",
				Family:  "elephant",
				Variant: "alpha",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferOpenRouterMetaModel(tt.input[len("openrouter/"):])
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}

func TestInferGenericOpenRouterModel(t *testing.T) {
	tests := []struct {
		name      string
		creator   string
		modelPart string
		expected  ModelKey
	}{
		{
			name:      "simple case",
			creator:   "testcreator",
			modelPart: "testmodel-lite",
			expected: ModelKey{
				Creator: "testcreator",
				Family:  "testmodel",
				Variant: "lite",
			},
		},
		{
			name:      "no variant",
			creator:   "testcreator",
			modelPart: "testmodel",
			expected: ModelKey{
				Creator: "testcreator",
				Family:  "testmodel",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, ok := inferGenericOpenRouterModel(tt.creator, tt.modelPart)
			require.True(t, ok)
			assert.Equal(t, NormalizeKey(tt.expected), key)
		})
	}
}
