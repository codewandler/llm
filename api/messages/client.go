package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codewandler/llm/api/apicore"
)

// Type aliases so callers write messages.Client, not apicore.Client[messages.Request].
type (
	Client       = apicore.Client[Request]
	ClientOption = apicore.ClientOption[Request]
)

// Re-exported option constructors with the type parameter locked to Request.
var (
	WithBaseURL      = apicore.WithBaseURL[Request]
	WithPath         = apicore.WithPath[Request]
	WithHTTPClient   = apicore.WithHTTPClient[Request]
	WithHeader       = apicore.WithHeader[Request]
	WithHeaderFunc   = apicore.WithHeaderFunc[Request]
	WithTransform    = apicore.WithTransform[Request]
	WithParseHook    = apicore.WithParseHook[Request]
	WithResponseHook = apicore.WithResponseHook[Request]
	WithErrorParser  = apicore.WithErrorParser[Request]
	WithLogger       = apicore.WithLogger[Request]
)

// NewClient creates a Messages API client with protocol defaults.
func NewClient(opts ...ClientOption) *Client {
	defaults := []ClientOption{
		WithPath(DefaultPath),
		WithHeader(HeaderAnthropicVersion, APIVersion),
		WithErrorParser(parseAPIError),
	}
	return apicore.NewClient[Request](NewParser(), append(defaults, opts...)...)
}

func parseAPIError(statusCode int, body []byte) error {
	var resp struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Error.Message == "" {
		return &apicore.HTTPError{StatusCode: statusCode, Body: body}
	}
	if resp.Error.Type != "" {
		return fmt.Errorf("%s: %s (HTTP %d)", resp.Error.Type, resp.Error.Message, statusCode)
	}
	return fmt.Errorf("%s (HTTP %d)", resp.Error.Message, statusCode)
}

// AuthHeaderFunc returns a HeaderFunc that sets x-api-key.
func AuthHeaderFunc(apiKey string) apicore.HeaderFunc[Request] {
	return func(_ context.Context, _ *Request) (http.Header, error) {
		return http.Header{HeaderAPIKey: {apiKey}}, nil
	}
}
