package ollama

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/usage"
)

func TestCreateStream_TextResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, responsesPath, r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: response.created\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"llama3.2\"}}\n\n" +
				"event: response.output_text.delta\ndata: {\"output_index\":0,\"delta\":\"Hello\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"output_index\":0,\"delta\":\" world\"}\n\n" +
				"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"llama3.2\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n",
		))
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL))
	stream, err := p.CreateStream(context.Background(), llm.Request{
		Model:    "llama3.2",
		Messages: llm.Messages{llm.User("hi")},
	})
	require.NoError(t, err)

	var (
		texts      []string
		sawStarted bool
		sawUsage   bool
		completed  *llm.CompletedEvent
	)
	for ev := range stream {
		switch ev.Type {
		case llm.StreamEventStarted:
			sawStarted = true
			se := ev.Data.(*llm.StreamStartedEvent)
			assert.Equal(t, llm.ProviderNameOllama, se.Provider)
			assert.Equal(t, "llama3.2", se.Model)
		case llm.StreamEventDelta:
			de := ev.Data.(*llm.DeltaEvent)
			if de.Kind == llm.DeltaKindText {
				texts = append(texts, de.Text)
			}
		case llm.StreamEventUsageUpdated:
			sawUsage = true
			ue := ev.Data.(*llm.UsageUpdatedEvent)
			assert.Equal(t, 12, ue.Record.Tokens.Count(usage.KindInput))
			assert.Equal(t, 3, ue.Record.Tokens.Count(usage.KindOutput))
		case llm.StreamEventCompleted:
			completed = ev.Data.(*llm.CompletedEvent)
		}
	}

	assert.True(t, sawStarted)
	assert.Equal(t, []string{"Hello", " world"}, texts)
	assert.True(t, sawUsage)
	require.NotNil(t, completed)
	assert.Equal(t, llm.StopReasonEndTurn, completed.StopReason)
}

func TestCreateStream_ToolCallResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(
			"event: response.created\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"llama3.2\"}}\n\n" +
				"event: response.function_call_arguments.delta\ndata: {\"item_id\":\"call_1\",\"output_index\":0,\"delta\":\"{\\\"city\\\":\"}\n\n" +
				"event: response.function_call_arguments.done\ndata: {\"item_id\":\"call_1\",\"output_index\":0,\"name\":\"lookup\",\"arguments\":\"{\\\"city\\\":\\\"Berlin\\\"}\"}\n\n" +
				"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"model\":\"llama3.2\",\"status\":\"completed\",\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n",
		))
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL))
	stream, err := p.CreateStream(context.Background(), llm.Request{
		Model:    "llama3.2",
		Messages: llm.Messages{llm.User("weather")},
	})
	require.NoError(t, err)

	var toolCall *llm.ToolCallEvent
	for ev := range stream {
		if ev.Type == llm.StreamEventToolCall {
			toolCall = ev.Data.(*llm.ToolCallEvent)
		}
	}

	require.NotNil(t, toolCall)
	assert.Equal(t, "call_1", toolCall.ToolCall.ToolCallID())
	assert.Equal(t, "lookup", toolCall.ToolCall.ToolName())
	assert.Equal(t, "Berlin", toolCall.ToolCall.ToolArgs()["city"])
}

func TestModels_VisibleRuntimeModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tags", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen2.5:0.5b"},{"name":"custom-model"}]}`))
	}))
	defer server.Close()

	p := New(llm.WithBaseURL(server.URL))
	models := p.Models()

	_, hasInstalled := models.ByID("qwen2.5:0.5b")
	_, hasCustom := models.ByID("custom-model")
	_, hasVisibleKnown := models.ByID("glm-4.7-flash")
	assert.True(t, hasInstalled)
	assert.True(t, hasCustom)
	assert.True(t, hasVisibleKnown)

	resolved, err := p.Resolve("glm-4.7-flash")
	require.NoError(t, err)
	assert.Equal(t, "glm-4.7-flash", resolved.ID)
	assert.Equal(t, llm.ProviderNameOllama, resolved.Provider)
}

func TestResolve_PassthroughUnknownModel(t *testing.T) {
	t.Parallel()

	p := New(llm.WithBaseURL("http://127.0.0.1:1"))
	resolved, err := p.Resolve("my-local-model")
	require.NoError(t, err)
	assert.Equal(t, "my-local-model", resolved.ID)
	assert.Equal(t, llm.ProviderNameOllama, resolved.Provider)
}
