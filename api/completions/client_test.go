package completions

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

func TestNewClient_WiresDefaultErrorParser(t *testing.T) {
	httpClient := &http.Client{Transport: apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 429,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"type":"rate_limit_error","message":"slow down"}}`)),
		}, nil
	})}

	c := NewClient(WithBaseURL("https://api.example.com"), WithHTTPClient(httpClient))
	_, err := c.Stream(context.Background(), &Request{Model: "gpt-4o", Stream: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit_error")
	assert.Contains(t, err.Error(), "slow down")
}

func TestParseAPIError_WithoutType(t *testing.T) {
	err := parseAPIError(400, []byte(`{"error":{"message":"bad request"}}`))
	require.Error(t, err)
	assert.Equal(t, "bad request (HTTP 400)", err.Error())
}

func TestMessageContent_StringOrArray_RoundTrip(t *testing.T) {
	arrReq := Request{
		Model:  "gpt-4o",
		Stream: true,
		Messages: []Message{{
			Role: "user",
			Content: []map[string]any{{
				"type": "text",
				"text": "hello",
			}},
		}},
	}
	arrBody, err := json.Marshal(arrReq)
	require.NoError(t, err)
	var arrDecoded map[string]any
	require.NoError(t, json.Unmarshal(arrBody, &arrDecoded))
	messages := arrDecoded["messages"].([]any)
	msg0 := messages[0].(map[string]any)
	parts := msg0["content"].([]any)
	part0 := parts[0].(map[string]any)
	assert.Equal(t, "text", part0["type"])
	assert.Equal(t, "hello", part0["text"])

	strReq := Request{
		Model:    "gpt-4o",
		Stream:   true,
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	strBody, err := json.Marshal(strReq)
	require.NoError(t, err)
	var strDecoded map[string]any
	require.NoError(t, json.Unmarshal(strBody, &strDecoded))
	strMessages := strDecoded["messages"].([]any)
	strMsg0 := strMessages[0].(map[string]any)
	assert.Equal(t, "hello", strMsg0["content"])
}
