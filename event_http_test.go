package llm

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderRequestFromHTTP_RedactsCredentialHeaders(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet"}`)
	r, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer sk-very-secret")
	r.Header.Set("X-Api-Key", "ak-very-secret")
	r.Header.Set("Anthropic-Version", "2023-06-01")

	pr := ProviderRequestFromHTTP(r, body)

	assert.Equal(t, "[REDACTED]", pr.Headers["Authorization"], "Authorization must be redacted")
	assert.Equal(t, "[REDACTED]", pr.Headers["X-Api-Key"], "X-Api-Key must be redacted")
	assert.Equal(t, "application/json", pr.Headers["Content-Type"], "non-sensitive headers must pass through")
	assert.Equal(t, "2023-06-01", pr.Headers["Anthropic-Version"], "non-sensitive headers must pass through")
	assert.Equal(t, "https://api.anthropic.com/v1/messages", pr.URL)
	assert.Equal(t, "POST", pr.Method)
	assert.JSONEq(t, `{"model":"claude-sonnet"}`, string(pr.Body))
}

func TestProviderRequestFromHTTP_NoCredentialHeaders(t *testing.T) {
	body := []byte(`{"model":"x"}`)
	r, err := http.NewRequest("GET", "https://example.com/health", bytes.NewReader(body))
	require.NoError(t, err)
	r.Header.Set("Content-Type", "application/json")

	pr := ProviderRequestFromHTTP(r, body)

	assert.Equal(t, "application/json", pr.Headers["Content-Type"])
	assert.NotContains(t, pr.Headers, "Authorization")
	assert.NotContains(t, pr.Headers, "X-Api-Key")
}
