package llm

import (
	"encoding/json"
	"net/http"
	"strings"
)

// redactedHeaders contains the canonical MIME header names whose values are
// replaced with "[REDACTED]" in ProviderRequest.Headers.
var redactedHeaders = map[string]bool{
	"Authorization": true,
	"X-Api-Key":     true,
}

// ProviderRequestFromHTTP builds a ProviderRequest from an outgoing *http.Request
// and the raw body bytes. Call it after the http.Request is fully constructed
// (all headers set) but before client.Do — the captured data then exactly matches
// what is sent on the wire.
//
// Header keys are in canonical MIME form (e.g. "Content-Type", "X-Api-Key") as
// stored by net/http. Multi-value headers are joined with ", ".
// Sensitive headers (Authorization, X-Api-Key) are replaced with "[REDACTED]".
//
// body is captured from the []byte slice, NOT from req.Body, so the reader
// inside req is untouched and client.Do works correctly afterwards.
func ProviderRequestFromHTTP(req *http.Request, body []byte) ProviderRequest {
	headers := make(map[string]string, len(req.Header))
	for k, v := range req.Header {
		if redactedHeaders[k] {
			headers[k] = "[REDACTED]"
		} else {
			headers[k] = strings.Join(v, ", ")
		}
	}
	return ProviderRequest{
		URL:     req.URL.String(),
		Method:  req.Method,
		Headers: headers,
		Body:    json.RawMessage(body),
	}
}
