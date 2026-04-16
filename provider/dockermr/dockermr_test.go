package dockermr_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/dockermr"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// drainEvents collects all events from a Stream channel and returns them in
// order. If an error event is received, it is returned as the second value.
func drainEvents(ch llm.Stream) ([]llm.Envelope, error) {
	var events []llm.Envelope
	for ev := range ch {
		events = append(events, ev)
		if ev.Type == llm.StreamEventError {
			if ee, ok := ev.Data.(*llm.ErrorEvent); ok {
				return events, ee.Error
			}
		}
	}
	return events, nil
}

// sseLines joins data lines into an SSE payload, appending a trailing newline.
func sseLines(lines ...string) string {
	// Insert a blank line between events so each payload is dispatched as a
	// separate SSE block, matching the behaviour of Docker Model Runner.
	return strings.Join(lines, "\n\n") + "\n\n"
}

// sseServer spins up an httptest.Server that serves the given SSE body for any
// POST to /engines/llama.cpp/v1/chat/completions.
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/engines/llama.cpp/v1/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
}

// ---------------------------------------------------------------------------
// Provider basics
// ---------------------------------------------------------------------------

func TestNew_Defaults(t *testing.T) {
	p := dockermr.New()
	assert.Equal(t, "dockermr", p.Name())
	assert.NotEmpty(t, p.Models())
}

func TestModels_AllHaveCorrectProvider(t *testing.T) {
	for _, m := range dockermr.New().Models() {
		assert.Equal(t, "dockermr", m.Provider, "model %s should have Provider == dockermr", m.ID)
		assert.NotEmpty(t, m.ID)
	}
}

func TestModels_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range dockermr.New().Models() {
		ids[m.ID] = true
	}
	assert.True(t, ids[dockermr.ModelSmoLLM2])
	assert.True(t, ids[dockermr.ModelQwen25])
	assert.True(t, ids[dockermr.ModelLlama32])
}

// ---------------------------------------------------------------------------
// FetchModels
// ---------------------------------------------------------------------------

func TestFetchModels_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/engines/llama.cpp/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "ai/smollm2"},
				{"id": "ai/qwen2.5"},
			},
		})
	}))
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	models, err := p.FetchModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "ai/smollm2", models[0].ID)
	assert.Equal(t, "dockermr", models[0].Provider, "FetchModels should relabel provider as dockermr")
	assert.Equal(t, "dockermr", models[1].Provider)
}

func TestFetchModels_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	_, err := p.FetchModels(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrAPIError)
}

// ---------------------------------------------------------------------------
// CreateStream — text content
// ---------------------------------------------------------------------------

func TestCreateStream_TextDeltas(t *testing.T) {
	sseData := sseLines(
		`data: {"id":"chatcmpl-1","model":"ai/smollm2","choices":[{"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","model":"ai/smollm2","choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","model":"ai/smollm2","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		`data: [DONE]`,
	)
	srv := sseServer(t, sseData)
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{llm.User("hi")}})
	require.NoError(t, err)

	events, streamErr := drainEvents(stream)
	require.NoError(t, streamErr)

	var texts []string
	var gotStarted, gotCompleted bool
	var completedEvent *llm.CompletedEvent
	for _, ev := range events {
		switch ev.Type {
		case llm.StreamEventStarted:
			gotStarted = true
			se := ev.Data.(*llm.StreamStartedEvent)
			assert.Equal(t, "ai/smollm2", se.Model)
			assert.Equal(t, "chatcmpl-1", se.RequestID)
			// The inner openai provider now carries p.Name() through the meta,
			// so the provider field in started events should say "dockermr".
			assert.Equal(t, "dockermr", se.Provider)
		case llm.StreamEventDelta:
			if de, ok := ev.Data.(*llm.DeltaEvent); ok && de.Kind == llm.DeltaKindText {
				texts = append(texts, de.Text)
			}
		case llm.StreamEventCompleted:
			gotCompleted = true
			completedEvent = ev.Data.(*llm.CompletedEvent)
		}
	}

	assert.True(t, gotStarted)
	assert.Equal(t, []string{"Hello", " world"}, texts)
	assert.True(t, gotCompleted)
	assert.Equal(t, llm.StopReasonEndTurn, completedEvent.StopReason)
}

func TestCreateStream_UsageRecord(t *testing.T) {
	sseData := sseLines(
		`data: {"id":"r1","model":"ai/smollm2","choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`,
		`data: {"id":"r1","model":"ai/smollm2","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
		`data: [DONE]`,
	)
	srv := sseServer(t, sseData)
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{llm.User("hi")}})
	require.NoError(t, err)

	events, streamErr := drainEvents(stream)
	require.NoError(t, streamErr)

	var rec *llm.UsageUpdatedEvent
	for _, ev := range events {
		if ev.Type == llm.StreamEventUsageUpdated {
			rec = ev.Data.(*llm.UsageUpdatedEvent)
		}
	}
	require.NotNil(t, rec, "UsageRecord should be emitted")
	assert.Equal(t, "dockermr", rec.Record.Dims.Provider)
	assert.Equal(t, "ai/smollm2", rec.Record.Dims.Model)
	assert.Equal(t, 12, rec.Record.Tokens.Count(usage.KindInput))
	assert.Equal(t, 3, rec.Record.Tokens.Count(usage.KindOutput))
}

// ---------------------------------------------------------------------------
// CreateStream — tool calls
// ---------------------------------------------------------------------------

func TestCreateStream_ToolCallsEmitted(t *testing.T) {
	sseData := sseLines(
		`data: {"id":"r2","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"r2","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"location\""}}]},"finish_reason":null}]}`,
		`data: {"id":"r2","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Paris\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"r2","model":"ai/smollm2","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	srv := sseServer(t, sseData)
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{llm.User("weather?")}})
	require.NoError(t, err)

	var toolDeltas []*llm.DeltaEvent
	var toolCalls []tool.Call
	var completedEvent *llm.CompletedEvent
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventDelta:
			if de, ok := ev.Data.(*llm.DeltaEvent); ok && de.Kind == llm.DeltaKindTool {
				toolDeltas = append(toolDeltas, de)
			}
		case llm.StreamEventToolCall:
			if tc, ok := ev.Data.(*llm.ToolCallEvent); ok {
				toolCalls = append(toolCalls, tc.ToolCall)
			}
		case llm.StreamEventCompleted:
			completedEvent = ev.Data.(*llm.CompletedEvent)
		}
	}

	var nonEmptyFragments int
	for _, td := range toolDeltas {
		if td.ToolArgs != "" {
			nonEmptyFragments++
		}
	}
	// Two non-empty argument fragments → two ToolDelta events.
	assert.Equal(t, 2, nonEmptyFragments)

	require.Len(t, toolCalls, 1)
	assert.Equal(t, "call_abc", toolCalls[0].ToolCallID())
	assert.Equal(t, "get_weather", toolCalls[0].ToolName())
	assert.Equal(t, "Paris", toolCalls[0].ToolArgs()["location"])

	require.NotNil(t, completedEvent)
	assert.Equal(t, llm.StopReasonToolUse, completedEvent.StopReason)
}

func TestCreateStream_ParallelToolCallsSortedByIndex(t *testing.T) {
	sseData := sseLines(
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"alpha","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":2,"id":"call_2","type":"function","function":{"name":"gamma","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_1","type":"function","function":{"name":"beta","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":1}"}}]},"finish_reason":null}]}`,
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"y\":2}"}}]},"finish_reason":null}]}`,
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{"tool_calls":[{"index":2,"function":{"arguments":"{\"z\":3}"}}]},"finish_reason":null}]}`,
		`data: {"id":"r3","model":"ai/smollm2","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	srv := sseServer(t, sseData)
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{llm.User("parallel")}})
	require.NoError(t, err)

	var toolCalls []tool.Call
	for ev := range stream {
		if ev.Type == llm.StreamEventToolCall {
			if tc, ok := ev.Data.(*llm.ToolCallEvent); ok {
				toolCalls = append(toolCalls, tc.ToolCall)
			}
		}
	}

	require.Len(t, toolCalls, 3)
	assert.Equal(t, "call_0", toolCalls[0].ToolCallID())
	assert.Equal(t, "alpha", toolCalls[0].ToolName())
	assert.Equal(t, float64(1), toolCalls[0].ToolArgs()["x"])
	assert.Equal(t, "call_1", toolCalls[1].ToolCallID())
	assert.Equal(t, "beta", toolCalls[1].ToolName())
	assert.Equal(t, "call_2", toolCalls[2].ToolCallID())
	assert.Equal(t, "gamma", toolCalls[2].ToolName())
}

// ---------------------------------------------------------------------------
// CreateStream — error paths
// ---------------------------------------------------------------------------

func TestCreateStream_ContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close() //nolint:errcheck

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.Copy(w, pr) // blocks until pipe is closed
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(ctx, llm.Request{Model: "ai/smollm2", Messages: llm.Messages{llm.User("ping")}})
	require.NoError(t, err)

	go func() {
		<-ctx.Done()
		_ = pw.CloseWithError(context.Canceled)
	}()

	cancel() // cancel while stream is open

	events, streamErr := drainEvents(stream)
	if streamErr != nil {
		require.ErrorIs(t, streamErr, llm.ErrContextCancelled)
		return
	}

	var sawCompleted bool
	for _, ev := range events {
		if ev.Type == llm.StreamEventCompleted {
			sawCompleted = true
			break
		}
	}
	assert.True(t, sawCompleted, "expected stream to finish after cancellation")
}

func TestCreateStream_Non200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	_, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{llm.User("hi")}})
	require.Error(t, err)
	assert.ErrorIs(t, err, llm.ErrAPIError)
}

// ---------------------------------------------------------------------------
// Request shape — verify the body sent to DMR
// ---------------------------------------------------------------------------

func TestCreateStream_RequestBody(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"id\":\"x\",\"model\":\"ai/smollm2\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n",
		)
	}))
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{
		llm.System("Be helpful."),
		llm.User("Hello"),
	}, MaxTokens: 256, Temperature: 0.7})
	require.NoError(t, err)
	for range stream {
	}

	require.NotEmpty(t, captured)
	var body map[string]any
	require.NoError(t, json.Unmarshal(captured, &body))

	assert.Equal(t, "ai/smollm2", body["model"])
	assert.Equal(t, true, body["stream"])
	assert.Equal(t, float64(256), body["max_tokens"])
	assert.InDelta(t, 0.7, body["temperature"], 1e-9)

	messages := body["messages"].([]any)
	require.Len(t, messages, 2)
	assert.Equal(t, "system", messages[0].(map[string]any)["role"])
	assert.Equal(t, "user", messages[1].(map[string]any)["role"])
}

func TestCreateStream_ToolResultsExpanded(t *testing.T) {
	// Tool results from a single RoleTool message should each become their
	// own "tool" role message in the wire format.
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"id\":\"x\",\"model\":\"ai/smollm2\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\ndata: [DONE]\n",
		)
	}))
	defer srv.Close()

	p := dockermr.New(llm.WithBaseURL(srv.URL + "/engines/llama.cpp"))
	stream, err := p.CreateStream(context.Background(), llm.Request{Model: "ai/smollm2", Messages: llm.Messages{
		llm.User("call a tool"),
		msg.Tool().Results(msg.ToolResult{ToolCallID: "call_1", ToolOutput: "result_1"}).Build(),
	}})
	require.NoError(t, err)
	for range stream {
	}

	var body map[string]any
	require.NoError(t, json.Unmarshal(captured, &body))
	messages := body["messages"].([]any)
	require.Len(t, messages, 2)
	toolMsg := messages[1].(map[string]any)
	assert.Equal(t, "tool", toolMsg["role"])
	assert.Equal(t, "call_1", toolMsg["tool_call_id"])
}

// ---------------------------------------------------------------------------
// Available — probe function
// ---------------------------------------------------------------------------

func TestAvailable_Returns200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/engines/llama.cpp/v1/models", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	// Redirect the probe to our test server via a custom transport.
	assert.True(t, dockermr.Available(&hostRewriteTransport{target: srv.URL}))
}

func TestAvailable_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	assert.False(t, dockermr.Available(&hostRewriteTransport{target: srv.URL}))
}

// hostRewriteTransport redirects requests aimed at DefaultBaseURL to an
// httptest.Server by overwriting the scheme and host.
type hostRewriteTransport struct{ target string }

func (h *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	parsed, err := http.NewRequest(req.Method, h.target+req.URL.Path, req.Body)
	if err != nil {
		return nil, err
	}
	clone.URL = parsed.URL
	return http.DefaultTransport.RoundTrip(clone)
}
