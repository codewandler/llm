package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentmessages "github.com/codewandler/agentapis/api/messages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestCreateStream_ValidateError(t *testing.T) {
	p := New(llm.WithAPIKey("key"))
	_, err := p.CreateStream(context.Background(), llm.Request{})
	require.Error(t, err)
	var pe *llm.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.ErrorIs(t, pe.Sentinel, llm.ErrBuildRequest)
}

func TestCreateStream_MissingAPIKey(t *testing.T) {
	p := New(llm.WithAPIKey(""))
	req := llm.Request{Model: "claude-sonnet-4-5", Messages: llm.Messages{llm.User("hi")}}
	_, err := p.CreateStream(context.Background(), req)
	require.Error(t, err)
	var pe *llm.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.ErrorIs(t, pe.Sentinel, llm.ErrMissingAPIKey)
}

func TestCreateStream_NonOKResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"too many requests"}}`)
	}))
	t.Cleanup(srv.Close)

	p := New(llm.WithAPIKey("test-key"), llm.WithBaseURL(srv.URL))
	req := llm.Request{Model: "claude-sonnet-4-5", Messages: llm.Messages{llm.User("hi")}}
	_, err := p.CreateStream(context.Background(), req)
	require.Error(t, err)
	var pe *llm.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.ErrorIs(t, pe.Sentinel, llm.ErrAPIError)
	assert.Equal(t, 429, pe.StatusCode)
}

func TestCreateStream_NetworkError(t *testing.T) {
	p := New(
		llm.WithAPIKey("test-key"),
		llm.WithHTTPClient(&http.Client{Transport: errorTransport{}}),
	)
	req := llm.Request{Model: "claude-sonnet-4-5", Messages: llm.Messages{llm.User("hi")}}
	_, err := p.CreateStream(context.Background(), req)
	require.Error(t, err)
	var pe *llm.ProviderError
	require.ErrorAs(t, err, &pe)
	assert.ErrorIs(t, pe.Sentinel, llm.ErrRequestFailed)
}

func TestCreateStream_HappyPath(t *testing.T) {
	// Fill the stop reason and usage explicitly after construction because the
	// generated struct uses embedded anonymous fields for those JSON sections.
	var messageDelta agentmessages.MessageDeltaEvent
	messageDelta.Delta.StopReason = agentmessages.StopReasonEndTurn
	messageDelta.Usage.OutputTokens = 3
	rawSSE, err := io.ReadAll(buildMessagesSSE(
		agentmessages.EventMessageStart,
		agentmessages.MessageStartEvent{Message: agentmessages.MessageStartPayload{
			ID:    "msg_01",
			Model: "claude-sonnet-4-5",
			Usage: agentmessages.MessageUsage{InputTokens: 10},
		}},
		agentmessages.EventContentBlockStart,
		agentmessages.ContentBlockStartEvent{
			Index:        0,
			ContentBlock: json.RawMessage(`{"type":"text","text":""}`),
		},
		agentmessages.EventContentBlockDelta,
		agentmessages.ContentBlockDeltaEvent{
			Index: 0,
			Delta: agentmessages.Delta{Type: agentmessages.DeltaTypeText, Text: "Hello!"},
		},
		agentmessages.EventContentBlockStop,
		agentmessages.ContentBlockStopEvent{Index: 0},
		agentmessages.EventMessageDelta,
		messageDelta,
		agentmessages.EventMessageStop,
		agentmessages.MessageStopEvent{},
	))
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, anthropicVersion, r.Header.Get("Anthropic-Version"))
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rawSSE)
	}))
	t.Cleanup(srv.Close)

	p := New(llm.WithAPIKey("test-key"), llm.WithBaseURL(srv.URL))
	req := llm.Request{Model: "claude-sonnet-4-5", Messages: llm.Messages{llm.User("hi")}}
	ch, err := p.CreateStream(context.Background(), req)
	require.NoError(t, err)

	var texts []string
	for env := range ch {
		if env.Type == llm.StreamEventDelta {
			if d, ok := env.Data.(*llm.DeltaEvent); ok && d.Text != "" {
				texts = append(texts, d.Text)
			}
		}
	}
	assert.Equal(t, []string{"Hello!"}, texts)
}

// errorTransport always returns a transport-level error.
type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("simulated network failure")
}

func buildMessagesSSE(parts ...any) io.ReadCloser {
	if len(parts)%2 != 0 {
		panic("buildMessagesSSE expects alternating event names and payloads")
	}
	var b strings.Builder
	for i := 0; i < len(parts); i += 2 {
		name, ok := parts[i].(string)
		if !ok {
			panic(fmt.Sprintf("buildMessagesSSE event name must be string, got %T", parts[i]))
		}
		data, err := json.Marshal(parts[i+1])
		if err != nil {
			panic(fmt.Sprintf("buildMessagesSSE marshal %s: %v", name, err))
		}
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", name, data)
	}
	return io.NopCloser(strings.NewReader(b.String()))
}

func TestCreateStream_AutoSystemCacheControl_Optional(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
		}))
		defer srv.Close()
		p := New(llm.WithAPIKey("test-key"), llm.WithBaseURL(srv.URL))
		stream, err := p.CreateStream(context.Background(), llm.Request{Model: "claude-sonnet-4-5", Messages: llm.Messages{llm.System("sys"), llm.User("hi")}})
		require.NoError(t, err)
		for range stream {
		}
		blocks := gotBody["system"].([]any)
		for _, b := range blocks {
			m := b.(map[string]any)
			assert.Nil(t, m["cache_control"])
		}
	})

	t.Run("enabled via option", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer r.Body.Close()
			require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "event: message_stop\ndata: {}\n\n")
		}))
		defer srv.Close()
		p := New(llm.WithAPIKey("test-key"), llm.WithBaseURL(srv.URL), WithAnthropicAutoSystemCacheControl("1h"))
		stream, err := p.CreateStream(context.Background(), llm.Request{Model: "claude-sonnet-4-5", Messages: llm.Messages{llm.System("sys"), llm.User("hi")}})
		require.NoError(t, err)
		for range stream {
		}
		blocks := gotBody["system"].([]any)
		cc := blocks[0].(map[string]any)["cache_control"].(map[string]any)
		assert.Equal(t, "ephemeral", cc["type"])
		assert.Equal(t, "1h", cc["ttl"])
	})
}
