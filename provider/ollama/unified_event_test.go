package ollama

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/completions"
	"github.com/codewandler/llm/api/unified"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnifiedCompletionsEventPipeline_OllamaCompatible(t *testing.T) {
	sseBody := "data: {\"id\":\"chatcmpl_1\",\"model\":\"llama3.2\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"model\":\"llama3.2\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\n" +
		"data: [DONE]\n\n"

	client := completions.NewClient(
		completions.WithBaseURL("https://fake.api"),
		completions.WithHTTPClient(&http.Client{Transport: ollamaRT(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseBody)),
			}, nil
		})}),
	)

	handle, err := client.Stream(context.Background(), &completions.Request{Model: "llama3.2", Stream: true})
	require.NoError(t, err)

	pub, ch := llm.NewEventPublisher()
	go func() {
		defer pub.Close()
		for r := range handle.Events {
			if r.Err != nil {
				pub.Error(r.Err)
				return
			}
			uEv, ignored, convErr := unified.EventFromCompletions(r.Event)
			if convErr != nil {
				pub.Error(convErr)
				return
			}
			if ignored {
				continue
			}
			if err := unified.Publish(pub, uEv); err != nil {
				pub.Error(err)
				return
			}
		}
	}()

	var sawCompleted bool
	for ev := range ch {
		if ev.Type == llm.StreamEventCompleted {
			sawCompleted = true
		}
	}
	assert.True(t, sawCompleted)
}

type ollamaRT func(*http.Request) (*http.Response, error)

func (f ollamaRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
