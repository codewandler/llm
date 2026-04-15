// api/apicore/client_test.go
package apicore_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/apicore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testReq struct {
	Model string `json:"model"`
}

func noopParser() apicore.EventHandler {
	return func(name string, data []byte) apicore.StreamResult {
		return apicore.StreamResult{Done: true}
	}
}

func sseBody(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func TestClient_Stream_URLComposition(t *testing.T) {
	var gotURL string
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithBaseURL[testReq]("https://api.example.com"),
		apicore.WithPath[testReq]("/v1/stream"),
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
	)
	_, err := c.Stream(context.Background(), &testReq{Model: "test"})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com/v1/stream", gotURL)
}

func TestClient_Stream_SerializesBody(t *testing.T) {
	var gotBody []byte
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
	)
	_, err := c.Stream(context.Background(), &testReq{Model: "gpt-4"})
	require.NoError(t, err)
	var parsed testReq
	require.NoError(t, json.Unmarshal(gotBody, &parsed))
	assert.Equal(t, "gpt-4", parsed.Model)
}

func TestClient_Stream_Non2xx_UsesErrorParser(t *testing.T) {
	transport := apicore.FixedSSEResponse(429, `{"error":"rate limited"}`)
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithErrorParser[testReq](func(status int, body []byte) error {
			return &apicore.HTTPError{StatusCode: status, Body: body}
		}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	var herr *apicore.HTTPError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, 429, herr.StatusCode)
}

func TestClient_Stream_Non2xx_DefaultHTTPError(t *testing.T) {
	transport := apicore.FixedSSEResponse(500, `internal error`)
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	var herr *apicore.HTTPError
	require.ErrorAs(t, err, &herr)
	assert.Equal(t, 500, herr.StatusCode)
}

func TestClient_Stream_EventsDeliveredInOrder(t *testing.T) {
	body := sseBody(
		"data: one",
		"",
		"data: two",
		"",
		"data: [DONE]",
		"",
	)
	transport := apicore.FixedSSEResponse(200, body)
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			s := string(data)
			if s == "[DONE]" {
				return apicore.StreamResult{Done: true}
			}
			return apicore.StreamResult{Event: s}
		}
	}, apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}))

	handle, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)

	var received []string
	for result := range handle.Events {
		if s, ok := result.Event.(string); ok {
			received = append(received, s)
		}
	}
	assert.Equal(t, []string{"one", "two"}, received)
}

func TestClient_Stream_ResponseHookReceivesMeta(t *testing.T) {
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"X-Request-Id": {"req-123"}, "Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	var gotMeta apicore.ResponseMeta
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithResponseHook[testReq](func(req *testReq, meta apicore.ResponseMeta) {
			gotMeta = meta
		}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)
	assert.Equal(t, 200, gotMeta.StatusCode)
	assert.Equal(t, "req-123", gotMeta.Headers.Get("X-Request-Id"))
}

func TestClient_Stream_StaticAndDynamicHeadersMerge(t *testing.T) {
	var gotHeaders http.Header
	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotHeaders = r.Header.Clone()
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})
	c := apicore.NewClient[testReq](func() apicore.EventHandler { return noopParser() },
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithHeader[testReq]("X-Static", "static-val"),
		apicore.WithHeaderFunc[testReq](func(ctx context.Context, req *testReq) (http.Header, error) {
			return http.Header{"X-Dynamic": {"dyn-val"}}, nil
		}),
	)
	_, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)
	assert.Equal(t, "static-val", gotHeaders.Get("X-Static"))
	assert.Equal(t, "dyn-val", gotHeaders.Get("X-Dynamic"))
}

func TestClient_Stream_WithLogger_DeliversEvents(t *testing.T) {
	transport := apicore.FixedSSEResponse(200, sseBody("data: hello", "", "data: [DONE]", ""))
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			if string(data) == "[DONE]" {
				return apicore.StreamResult{Done: true}
			}
			return apicore.StreamResult{Event: string(data)}
		}
	},
		apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}),
		apicore.WithLogger[testReq](logger),
	)
	handle, err := c.Stream(context.Background(), &testReq{Model: "test"})
	require.NoError(t, err)
	var events []any
	for result := range handle.Events {
		if result.Event != nil {
			events = append(events, result.Event)
		}
	}
	assert.Equal(t, []any{"hello"}, events)
}

func TestClient_Stream_NamedEvents(t *testing.T) {
	body := sseBody(
		"event: message_start",
		`data: {"id":"msg_1"}`,
		"",
		"event: content_block_delta",
		`data: {"text":"hello"}`,
		"",
		"event: message_stop",
		"data: {}",
		"",
	)
	transport := apicore.FixedSSEResponse(200, body)
	type event struct{ name, data string }
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			if name == "message_stop" {
				return apicore.StreamResult{Event: event{name, string(data)}, Done: true}
			}
			if name != "" {
				return apicore.StreamResult{Event: event{name, string(data)}}
			}
			return apicore.StreamResult{}
		}
	}, apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}))

	handle, err := c.Stream(context.Background(), &testReq{})
	require.NoError(t, err)

	var received []event
	for result := range handle.Events {
		if e, ok := result.Event.(event); ok {
			received = append(received, e)
		}
	}
	require.Len(t, received, 3)
	assert.Equal(t, "message_start", received[0].name)
	assert.Equal(t, "content_block_delta", received[1].name)
	assert.Equal(t, "message_stop", received[2].name)
}

func TestClient_Stream_ContextCancelled_ChannelCloses(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	transport := apicore.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       pr,
		}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	c := apicore.NewClient[testReq](func() apicore.EventHandler {
		return func(name string, data []byte) apicore.StreamResult {
			return apicore.StreamResult{Event: string(data)}
		}
	}, apicore.WithHTTPClient[testReq](&http.Client{Transport: transport}))

	handle, err := c.Stream(ctx, &testReq{})
	require.NoError(t, err)

	cancel() // signal cancellation; forEachDataLine will close the pipe reader

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range handle.Events {
		}
	}()

	select {
	case <-done:
		// handle.Events closed cleanly after context cancellation
	case <-time.After(2 * time.Second):
		t.Fatal("handle.Events did not close after context cancellation")
	}
}
