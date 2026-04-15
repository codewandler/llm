package completions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/codewandler/llm/api/apicore"
)

// Type aliases.
type (
	Client       = apicore.Client[Request]
	ClientOption = apicore.ClientOption[Request]
)

// Re-export option constructors with type parameter locked to Request.
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

// NewClient creates a Chat Completions client with protocol defaults.
// Caller supplies WithBaseURL and auth headers.
func NewClient(opts ...ClientOption) *Client {
	defaults := []ClientOption{
		WithPath(DefaultPath),
		WithErrorParser(parseAPIError),
	}
	return apicore.NewClient[Request](NewParser(), append(defaults, opts...)...)
}

// BearerAuthFunc returns a HeaderFunc that sets Authorization: Bearer <key>.
func BearerAuthFunc(apiKey string) apicore.HeaderFunc[Request] {
	return func(_ context.Context, _ *Request) (http.Header, error) {
		return http.Header{"Authorization": {"Bearer " + apiKey}}, nil
	}
}

// parseAPIError converts an OpenAI HTTP error response into a typed error.
// Ref: https://platform.openai.com/docs/guides/error-codes
func parseAPIError(statusCode int, body []byte) error {
	var resp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Error.Message == "" {
		return &apicore.HTTPError{StatusCode: statusCode, Body: body}
	}
	if resp.Error.Type != "" {
		return fmt.Errorf("%s: %s (HTTP %d)", resp.Error.Type, resp.Error.Message, statusCode)
	}
	return fmt.Errorf("openai error: %s (HTTP %d)", resp.Error.Message, statusCode)
}
