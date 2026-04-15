package completions

import (
	"context"
	"net/http"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_DefaultPath(t *testing.T) {
	var gotPath string
	httpClient := &http.Client{Transport: apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: http.NoBody}, nil
	})}
	c := NewClient(WithBaseURL("https://api.example.com"), WithHTTPClient(httpClient))
	_, err := c.Stream(context.Background(), &Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)
	assert.Equal(t, DefaultPath, gotPath)
}

func TestBearerAuthFunc(t *testing.T) {
	fn := BearerAuthFunc("sk-test")
	h, err := fn(context.Background(), &Request{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk-test", h.Get("Authorization"))
}

func TestParseAPIError_JSON(t *testing.T) {
	err := parseAPIError(429, []byte(`{"error":{"type":"rate_limit_error","message":"too many requests"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit_error")
	assert.Contains(t, err.Error(), "too many requests")
}

func TestParseAPIError_FallbackHTTPError(t *testing.T) {
	err := parseAPIError(500, []byte("not-json"))
	var hErr *apicore.HTTPError
	require.ErrorAs(t, err, &hErr)
	assert.Equal(t, 500, hErr.StatusCode)
}

func TestParseAPIError_EmptyMessage_FallbackHTTPError(t *testing.T) {
	err := parseAPIError(400, []byte(`{"error":{"type":"invalid_request_error","message":""}}`))
	var hErr *apicore.HTTPError
	require.ErrorAs(t, err, &hErr)
	assert.Equal(t, 400, hErr.StatusCode)
}
