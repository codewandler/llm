package completions_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codewandler/llm/api/apicore"
	"github.com/codewandler/llm/api/completions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeHandler() apicore.EventHandler {
	return completions.NewParser()()
}

func fixtureClient(t *testing.T, name string) *http.Client {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return &http.Client{Transport: apicore.FixedSSEResponse(200, string(data))}
}

func TestParser_TextChunk(t *testing.T) {
	h := makeHandler()
	data := []byte(`{"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":""}]}`)
	result := h("", data)
	require.NoError(t, result.Err)
	assert.False(t, result.Done)
	chunk, ok := result.Event.(*completions.Chunk)
	require.True(t, ok)
	require.Len(t, chunk.Choices, 1)
	assert.Equal(t, "hello", chunk.Choices[0].Delta.Content)
}

func TestParser_DoneSignal(t *testing.T) {
	h := makeHandler()
	result := h("", []byte(completions.StreamDone))
	assert.True(t, result.Done)
	assert.Nil(t, result.Event)
}

func TestParser_DoneSignal_IgnoresEventName(t *testing.T) {
	h := makeHandler()
	result := h("unexpected", []byte(completions.StreamDone))
	assert.True(t, result.Done)
}

func TestParser_FinalChunkWithUsage(t *testing.T) {
	h := makeHandler()
	data := []byte(`{"id":"c1","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	result := h("", data)
	require.NoError(t, result.Err)
	chunk := result.Event.(*completions.Chunk)
	require.NotNil(t, chunk.Usage)
	assert.Equal(t, 10, chunk.Usage.PromptTokens)
	assert.Equal(t, 5, chunk.Usage.CompletionTokens)
}

func TestParser_MalformedJSON_ReturnsError(t *testing.T) {
	h := makeHandler()
	result := h("", []byte(`{not valid json`))
	require.Error(t, result.Err)
	assert.False(t, result.Done)
}

func TestParser_IsolatedState(t *testing.T) {
	factory := completions.NewParser()
	h1, h2 := factory(), factory()
	data := []byte(`{"id":"c1","model":"m","choices":[]}`)
	r1 := h1("", data)
	r2 := h2("", data)
	require.NoError(t, r1.Err)
	require.NoError(t, r2.Err)
}

func TestParser_FixtureTextStream(t *testing.T) {
	c := fixtureClient(t, "text_stream.sse")
	client := completions.NewClient(
		completions.WithBaseURL("https://example.com"),
		completions.WithHTTPClient(c),
	)
	handle, err := client.Stream(t.Context(), &completions.Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)

	var text strings.Builder
	for ev := range handle.Events {
		require.NoError(t, ev.Err)
		if chunk, ok := ev.Event.(*completions.Chunk); ok && len(chunk.Choices) > 0 {
			text.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	assert.Equal(t, "Hello", text.String())
}

func TestParser_FixtureMalformedThenDone(t *testing.T) {
	c := fixtureClient(t, "malformed_stream.sse")
	client := completions.NewClient(
		completions.WithBaseURL("https://example.com"),
		completions.WithHTTPClient(c),
	)
	handle, err := client.Stream(t.Context(), &completions.Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)

	var sawErr, sawDone bool
	for ev := range handle.Events {
		if ev.Err != nil {
			sawErr = true
		}
		if ev.Done {
			sawDone = true
		}
	}
	assert.True(t, sawErr)
	assert.True(t, sawDone)
}

func TestParser_FixtureUsageChunk(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "usage_stream.sse"))
	require.NoError(t, err)
	h := makeHandler()

	var usage *completions.Usage
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		res := h("", []byte(payload))
		if chunk, ok := res.Event.(*completions.Chunk); ok && chunk.Usage != nil {
			usage = chunk.Usage
		}
	}
	require.NotNil(t, usage)
	require.NotNil(t, usage.PromptTokensDetails)
	require.NotNil(t, usage.CompletionTokensDetails)
	assert.Equal(t, 2, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 1, usage.CompletionTokensDetails.ReasoningTokens)
}

func TestParser_FinishReasonNull_Decodes(t *testing.T) {
	h := makeHandler()
	data := []byte(`{"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`)
	result := h("", data)
	require.NoError(t, result.Err)
	chunk, ok := result.Event.(*completions.Chunk)
	require.True(t, ok)
	require.Len(t, chunk.Choices, 1)
	assert.Nil(t, chunk.Choices[0].FinishReason)
	assert.Equal(t, "hi", chunk.Choices[0].Delta.Content)
}

func TestParser_FixtureToolStream(t *testing.T) {
	c := fixtureClient(t, "tool_stream.sse")
	client := completions.NewClient(
		completions.WithBaseURL("https://example.com"),
		completions.WithHTTPClient(c),
	)
	handle, err := client.Stream(t.Context(), &completions.Request{Model: "gpt-4o", Stream: true})
	require.NoError(t, err)

	var (
		argFragments []string
		finish       *string
		sawDone      bool
	)
	for ev := range handle.Events {
		require.NoError(t, ev.Err)
		if ev.Done {
			sawDone = true
			continue
		}
		chunk, ok := ev.Event.(*completions.Chunk)
		if !ok || len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != nil {
			finish = choice.FinishReason
		}
		if len(choice.Delta.ToolCalls) > 0 {
			argFragments = append(argFragments, choice.Delta.ToolCalls[0].Function.Arguments)
		}
	}
	require.True(t, sawDone)
	require.Equal(t, []string{"{\"loc\"", ":\"Berlin\"}"}, argFragments)
	require.NotNil(t, finish)
	assert.Equal(t, completions.FinishReasonToolCalls, *finish)
}
