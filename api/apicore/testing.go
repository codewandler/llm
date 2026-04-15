// api/apicore/testing.go
package apicore

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// RoundTripFunc adapts a plain function to http.RoundTripper.
// Use with WithHTTPClient to intercept HTTP in tests without a real server.
//
//	c := NewClient[Req](factory,
//	    WithHTTPClient[Req](&http.Client{Transport: RoundTripFunc(func(r *http.Request) (*http.Response, error) {
//	        return &http.Response{...}, nil
//	    })}),
//	)
type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// FixedSSEResponse returns a RoundTripFunc that always responds with the given
// status code and SSE body. Use for table-driven stream parser tests.
func FixedSSEResponse(statusCode int, sseBody string) RoundTripFunc {
	return func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: statusCode,
			Header:     http.Header{"Content-Type": {ContentTypeEventStream}},
			Body:       io.NopCloser(strings.NewReader(sseBody)),
		}, nil
	}
}

// NewTestHandle creates a StreamHandle populated with canned StreamResults.
// Use in adapter tests that bypass HTTP entirely.
//
//	handle := NewTestHandle(
//	    StreamResult{Event: &SomeEvent{...}},
//	    StreamResult{Done: true},
//	)
func NewTestHandle(events ...StreamResult) *StreamHandle {
	ch := make(chan StreamResult, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return &StreamHandle{
		Events:  ch,
		Request: httptest.NewRequest(http.MethodPost, "/test", nil),
		Headers: make(http.Header),
	}
}
