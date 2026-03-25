package llm_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestProviderError_Is(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"context cancelled", llm.NewErrContextCancelled(llm.ProviderNameAnthropic, errors.New("ctx")), llm.ErrContextCancelled},
		{"request failed", llm.NewErrRequestFailed(llm.ProviderNameOpenAI, errors.New("net")), llm.ErrRequestFailed},
		{"API error", llm.NewErrAPIError(llm.ProviderNameBedrock, 429, "too many requests"), llm.ErrAPIError},
		{"eventPub read", llm.NewErrStreamRead(llm.ProviderNameOllama, errors.New("eof")), llm.ErrStreamRead},
		{"eventPub decode", llm.NewErrStreamDecode(llm.ProviderNameOpenRouter, errors.New("bad json")), llm.ErrStreamDecode},
		{"provider msg", llm.NewErrProviderMsg(llm.ProviderNameAnthropic, "overloaded"), llm.ErrProviderError},
		{"missing api key", llm.NewErrMissingAPIKey(llm.ProviderNameOpenAI), llm.ErrMissingAPIKey},
		{"build request", llm.NewErrBuildRequest(llm.ProviderNameBedrock, errors.New("marshal")), llm.ErrBuildRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, errors.Is(tt.err, tt.sentinel),
				"errors.Is(%v, %v) should be true", tt.err, tt.sentinel)
			// Must not match a different sentinel
			assert.False(t, errors.Is(tt.err, llm.ErrMissingAPIKey) && tt.sentinel != llm.ErrMissingAPIKey,
				"error should not match wrong sentinel")
		})
	}
}

func TestProviderError_ErrorMessage(t *testing.T) {
	t.Run("API error includes status code", func(t *testing.T) {
		err := llm.NewErrAPIError(llm.ProviderNameAnthropic, 429, "rate limited")
		msg := err.Error()
		assert.Contains(t, msg, "anthropic")
		assert.Contains(t, msg, "429")
	})

	t.Run("request failed wraps cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := llm.NewErrRequestFailed(llm.ProviderNameOllama, cause)
		assert.Contains(t, err.Error(), "ollama")
		assert.Contains(t, err.Error(), "connection refused")
	})

	t.Run("provider msg includes message", func(t *testing.T) {
		err := llm.NewErrProviderMsg(llm.ProviderNameOpenRouter, "model not found")
		assert.Contains(t, err.Error(), "openrouter")
		assert.Contains(t, err.Error(), "model not found")
	})

	t.Run("missing API key has provider name", func(t *testing.T) {
		err := llm.NewErrMissingAPIKey(llm.ProviderNameOpenAI)
		assert.Contains(t, err.Error(), "openai")
	})
}

func TestProviderError_Unwrap(t *testing.T) {
	t.Run("cause is unwrapped when present", func(t *testing.T) {
		cause := errors.New("root cause")
		err := llm.NewErrStreamRead(llm.ProviderNameBedrock, cause)
		require.True(t, errors.Is(err, cause))
	})

	t.Run("sentinel is unwrapped when no cause", func(t *testing.T) {
		err := llm.NewErrMissingAPIKey(llm.ProviderNameAnthropic)
		require.True(t, errors.Is(err, llm.ErrMissingAPIKey))
	})
}

func TestProviderError_APIErrorFields(t *testing.T) {
	err := llm.NewErrAPIError(llm.ProviderNameOpenAI, 503, "service unavailable")
	var pe *llm.ProviderError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, 503, pe.StatusCode)
	assert.True(t, strings.Contains(pe.Body, "service unavailable"))
	assert.Equal(t, llm.ProviderNameOpenAI, pe.Provider)
}
