package tokencount

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodingForModel(t *testing.T) {
	tests := []struct {
		model     string
		wantEnc   string
		wantKnown bool
	}{
		// o200k_base
		{"gpt-4o", EncodingO200K, true},
		{"gpt-4o-mini", EncodingO200K, true},
		{"gpt-4o-2024-11-20", EncodingO200K, true},
		{"gpt-4.1", EncodingO200K, true},
		{"gpt-4.1-mini", EncodingO200K, true},
		{"gpt-4.5", EncodingO200K, true},
		{"o1", EncodingO200K, true},
		{"o1-mini", EncodingO200K, true},
		{"o3", EncodingO200K, true},
		{"o3-mini", EncodingO200K, true},
		{"o4-mini", EncodingO200K, true},
		// cl100k_base — Claude
		{"claude-3-5-sonnet-20241022", EncodingCL100K, true},
		{"claude-3-opus-20240229", EncodingCL100K, true},
		{"claude-sonnet-4-5", EncodingCL100K, true},
		// cl100k_base — GPT-4 (non-o)
		{"gpt-4", EncodingCL100K, true},
		{"gpt-4-turbo", EncodingCL100K, true},
		{"gpt-3.5-turbo", EncodingCL100K, true},
		// unknown → fallback cl100k_base
		{"llama3.2:1b", EncodingCL100K, false},
		{"mistral", EncodingCL100K, false},
		{"some-unknown-model", EncodingCL100K, false},
		// minimax_bpe
		{"minimax-m2.7", EncodingMinimax, true},
		{"minimax-m2.5-highspeed", EncodingMinimax, true},
		{"minimax-m2.1", EncodingMinimax, true},
		{"minimax-m2", EncodingMinimax, true},
		// negative — must NOT match minimax
		{"my-not-minimax-provider", EncodingCL100K, false},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			enc, known := EncodingForModel(tc.model)
			assert.Equal(t, tc.wantEnc, enc)
			assert.Equal(t, tc.wantKnown, known)
		})
	}
}

func TestCountText_CL100K(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantTokens int
	}{
		// Well-known tiktoken values for cl100k_base
		{"hello world", "Hello, world!", 4},
		{"empty", "", 0},
		{"single word", "hello", 1},
		{"numbers", "1234", 2}, // cl100k_base splits "1234" into two tokens
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CountTextForEncoding(EncodingCL100K, tc.text)
			require.NoError(t, err)
			assert.Equal(t, tc.wantTokens, got)
		})
	}
}

func TestCountText_O200K(t *testing.T) {
	// o200k_base tokenises the same simple ASCII similarly to cl100k_base
	got, err := CountTextForEncoding(EncodingO200K, "Hello, world!")
	require.NoError(t, err)
	assert.Equal(t, 4, got)
}

func TestCountText_UnknownEncoding(t *testing.T) {
	_, err := CountTextForEncoding("not_a_real_encoding", "hello")
	require.Error(t, err)
}

func TestCountTextForModel(t *testing.T) {
	// Basic smoke test — delegates to CountTextForEncoding
	n, err := CountTextForModel("gpt-4o", "Hello")
	require.NoError(t, err)
	assert.Greater(t, n, 0)
}

func TestEncodingForModel_CaseInsensitive(t *testing.T) {
	enc1, _ := EncodingForModel("GPT-4O")
	enc2, _ := EncodingForModel("gpt-4o")
	assert.Equal(t, enc1, enc2)

	// MiniMax models are case-insensitive too
	enc3, _ := EncodingForModel("MiniMax-M2.7")
	enc4, _ := EncodingForModel("minimax-m2.7")
	assert.Equal(t, enc3, enc4)
	assert.Equal(t, EncodingMinimax, enc3)
}
