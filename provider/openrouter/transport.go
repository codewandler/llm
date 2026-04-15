package openrouter

import "net/http"

// singleResponseTransport is an http.RoundTripper that returns a pre-built
// *http.Response exactly once. Used to feed an already-received HTTP response
// body into a wire client (messages.Client, responses.Client) without making
// a second network call.
type singleResponseTransport struct {
	resp *http.Response
}

func (t *singleResponseTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return t.resp, nil
}
