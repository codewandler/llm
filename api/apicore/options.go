// api/apicore/options.go
package apicore

import (
	"log/slog"
	"net/http"
)

// ClientOption[Req] configures a Client[Req].
type ClientOption[Req any] func(*Client[Req])

func WithBaseURL[Req any](url string) ClientOption[Req] {
	return func(c *Client[Req]) { c.baseURL = url }
}

func WithPath[Req any](path string) ClientOption[Req] {
	return func(c *Client[Req]) { c.path = path }
}

func WithHTTPClient[Req any](client *http.Client) ClientOption[Req] {
	return func(c *Client[Req]) { c.httpClient = client }
}

// WithHeader sets a static header sent on every request.
func WithHeader[Req any](key, value string) ClientOption[Req] {
	return func(c *Client[Req]) { c.headers.Set(key, value) }
}

func WithHeaderFunc[Req any](fn HeaderFunc[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.headerFunc = fn }
}

func WithTransform[Req any](fn TransformFunc[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.transform = fn }
}

func WithParseHook[Req any](fn ParseHook[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.parseHook = fn }
}

func WithResponseHook[Req any](fn ResponseHook[Req]) ClientOption[Req] {
	return func(c *Client[Req]) { c.responseHook = fn }
}

func WithErrorParser[Req any](fn ErrorParser) ClientOption[Req] {
	return func(c *Client[Req]) { c.errParser = fn }
}

func WithLogger[Req any](logger *slog.Logger) ClientOption[Req] {
	return func(c *Client[Req]) { c.logger = logger }
}
