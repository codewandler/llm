package messages_test

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/messages"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	openRouterBaseURL = "https://openrouter.ai/api"
	testReferer       = "https://github.com/codewandler/llm"
	testTitle         = "llm-integration-test"
)

var knownRawEvents = map[string]struct{}{
	messages.EventMessageStart:      {},
	messages.EventContentBlockStart: {},
	messages.EventContentBlockDelta: {},
	messages.EventContentBlockStop:  {},
	messages.EventMessageDelta:      {},
	messages.EventMessageStop:       {},
	messages.EventPing:              {},
	messages.EventError:             {},
}

func shouldSkipOpenRouterError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "guardrail restrictions") ||
		strings.Contains(msg, "data policy") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "insufficient credits") ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "provider returned error (http 429)") {
		return true
	}
	return false
}

func requireOpenRouterVars(t *testing.T) (string, string) {
	t.Helper()
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := os.Getenv("OPENROUTER_MESSAGES_MODEL")
	if apiKey == "" || model == "" {
		t.Skip("requires OPENROUTER_API_KEY and OPENROUTER_MESSAGES_MODEL")
	}
	return apiKey, model
}

func TestIntegration_OpenRouter_MessagesStream(t *testing.T) {
	apiKey, model := requireOpenRouterVars(t)

	rawCounts := map[string]int{}
	client := messages.NewClient(
		messages.WithBaseURL(openRouterBaseURL),
		messages.WithHeader("Authorization", "Bearer "+apiKey),
		messages.WithHeader("HTTP-Referer", testReferer),
		messages.WithHeader("X-Title", testTitle),
		messages.WithParseHook(func(_ *messages.Request, eventName string, _ []byte) any {
			if eventName != "" {
				rawCounts[eventName]++
			}
			return nil
		}),
	)

	req := &messages.Request{
		Model:     model,
		MaxTokens: 64,
		Stream:    true,
		Messages:  []messages.Message{{Role: "user", Content: "Reply with exactly: pong"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handle, err := client.Stream(ctx, req)
	if shouldSkipOpenRouterError(err) {
		t.Skipf("skipping integration test due environment/account constraints: %v", err)
	}
	require.NoError(t, err)

	var (
		haveStart bool
		haveStop  bool
		text      strings.Builder
		streamErr error
	)
	handledCounts := map[string]int{}

	for result := range handle.Events {
		if result.Err != nil {
			streamErr = result.Err
			handledCounts[messages.EventError]++
			continue
		}
		switch ev := result.Event.(type) {
		case *messages.MessageStartEvent:
			haveStart = true
			handledCounts[messages.EventMessageStart]++
		case *messages.ContentBlockStartEvent:
			handledCounts[messages.EventContentBlockStart]++
		case *messages.ContentBlockDeltaEvent:
			handledCounts[messages.EventContentBlockDelta]++
		case *messages.ContentBlockStopEvent:
			handledCounts[messages.EventContentBlockStop]++
		case *messages.TextCompleteEvent:
			text.WriteString(ev.Text)
			// synthesized from content_block_stop; raw coverage still comes from
			// ContentBlockStopEvent handling above.
		case *messages.PingEvent:
			handledCounts[messages.EventPing]++
		case *messages.MessageDeltaEvent:
			handledCounts[messages.EventMessageDelta]++
		case *messages.MessageStopEvent:
			haveStop = true
			handledCounts[messages.EventMessageStop]++
		}
	}

	if shouldSkipOpenRouterError(streamErr) {
		t.Skipf("skipping integration test due environment/account constraints: %v", streamErr)
	}
	require.NoError(t, streamErr)
	assert.True(t, haveStart)
	assert.True(t, haveStop)
	assert.NotEmpty(t, strings.TrimSpace(text.String()))

	logUnknownRawEvents(t, rawCounts, handledCounts)
}

func logUnknownRawEvents(t *testing.T, rawCounts, handledCounts map[string]int) {
	t.Helper()
	var keys []string
	for k := range rawCounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unknown []string
	for _, name := range keys {
		remaining := rawCounts[name] - handledCounts[name]
		if remaining <= 0 {
			continue
		}
		if _, ok := knownRawEvents[name]; ok {
			t.Logf("observed known raw event: %s x%d", name, remaining)
			continue
		}
		t.Logf("observed UNKNOWN raw event: %s x%d", name, remaining)
		unknown = append(unknown, name)
	}

	if os.Getenv("MESSAGES_STRICT_EVENTS") == "1" {
		require.Empty(t, unknown, "strict mode: unknown raw events detected")
	}
}
