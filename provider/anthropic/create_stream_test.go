package anthropic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"too many requests"}}`)
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
	sseBody := buildSSEBody(
		MessageStartEvent{Message: MessageStartPayload{
			ID: "msg_01", Model: "claude-sonnet-4-5",
			Usage: MessageUsage{InputTokens: 10},
		}},
		ContentBlockStartEvent{Index: 0, ContentBlock: ContentBlock{Type: "text"}},
		ContentBlockDeltaEvent{Index: 0, Delta: ContentBlockDelta{Type: "text_delta", Text: "Hello!"}},
		ContentBlockStopEvent{Index: 0},
		MessageDeltaEvent{
			Delta: MessageDelta{StopReason: "end_turn"},
			Usage: OutputUsage{OutputTokens: 3},
		},
		MessageStopEvent{},
	)
	rawSSE, err := io.ReadAll(sseBody)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, anthropicVersion, r.Header.Get("Anthropic-Version"))
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(rawSSE)
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
