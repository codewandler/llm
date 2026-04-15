package messages

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_DefaultPathAndVersionHeader(t *testing.T) {
	var gotPath string
	var gotVersion string
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotVersion = r.Header.Get(HeaderAnthropicVersion)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	c := NewClient(
		WithBaseURL("https://api.anthropic.com"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, err := c.Stream(context.Background(), &Request{
		Model:     "claude-3-5-haiku-20241022",
		MaxTokens: 16,
		Stream:    true,
		Messages:  []Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)

	assert.Equal(t, DefaultPath, gotPath)
	assert.Equal(t, APIVersion, gotVersion)
}

func TestParseAPIError_AnthropicShape(t *testing.T) {
	err := parseAPIError(400, []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad input"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_request_error")
	assert.Contains(t, err.Error(), "bad input")
	assert.Contains(t, err.Error(), "HTTP 400")
}

func TestParseAPIError_FallbackHTTPError(t *testing.T) {
	err := parseAPIError(502, []byte(`not-json`))
	var herr *apicore.HTTPError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, 502, herr.StatusCode)
	assert.Equal(t, []byte(`not-json`), herr.Body)
}
