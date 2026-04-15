package responses_test

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm/api/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Optional env overrides for model selection:
//
//	OPENROUTER_RESPONSES_FREE_MODEL
//	OPENROUTER_RESPONSES_TOOL_MODEL
//
// Optional strict mode for raw event coverage:
//
//	RESPONSES_STRICT_EVENTS=1   // fail test on unknown unhandled raw SSE event

const (
	openRouterBaseURL = "https://openrouter.ai/api"
	testReferer       = "https://github.com/codewandler/llm"
	testTitle         = "llm-integration-test"
)

var knownNoOpEvents = map[string]struct{}{
	"response.in_progress":                  {},
	"response.content_part.added":           {},
	"response.content_part.done":            {},
	"response.output_text.done":             {},
	"response.output_text.annotation.added": {},
	"response.function_call_arguments.done": {},
	"response.reasoning.delta":              {},
	"response.reasoning.done":               {},
	"response.reasoning_summary_text.done":  {},
	"response.queued":                       {},
	"rate_limits.updated":                   {},
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

func requireOpenRouterAPIKey(t *testing.T) string {
	t.Helper()
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set — skipping integration test")
	}
	return apiKey
}

func envOrDefault(name, fallback string) string {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	return v
}

type streamReport struct {
	createdEvt    *responses.ResponseCreatedEvent
	completedEvt  *responses.ResponseCompletedEvent
	textDeltas    []string
	reasoningSeen int
	toolCalls     []*responses.ToolCompleteEvent
	streamErrs    []error
	rawEvents     map[string]int
	handledEvents map[string]int
}

func newOpenRouterClient(apiKey string, onRawEvent func(name string)) *responses.Client {
	return responses.NewClient(
		responses.WithBaseURL(openRouterBaseURL),
		responses.WithHeaderFunc(responses.BearerAuthFunc(apiKey)),
		responses.WithHeader("HTTP-Referer", testReferer),
		responses.WithHeader("X-Title", testTitle),
		responses.WithParseHook(func(_ *responses.Request, eventName string, _ []byte) any {
			if eventName != "" && onRawEvent != nil {
				onRawEvent(eventName)
			}
			return nil
		}),
	)
}

func streamAndCollect(t *testing.T, client *responses.Client, req *responses.Request) streamReport {
	t.Helper()

	report := streamReport{
		rawEvents:     make(map[string]int),
		handledEvents: make(map[string]int),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	handle, err := client.Stream(ctx, req)
	if shouldSkipOpenRouterError(err) {
		t.Skipf("skipping integration test due environment/account constraints: %v", err)
	}
	require.NoError(t, err)

	for result := range handle.Events {
		if result.Err != nil {
			report.streamErrs = append(report.streamErrs, result.Err)
			report.handledEvents[responses.EventAPIError]++
			continue
		}

		switch ev := result.Event.(type) {
		case *responses.ResponseCreatedEvent:
			report.createdEvt = ev
			report.handledEvents[responses.EventResponseCreated]++
		case *responses.OutputItemAddedEvent:
			report.handledEvents[responses.EventOutputItemAdded]++
		case *responses.ReasoningDeltaEvent:
			report.reasoningSeen++
			report.handledEvents[responses.EventReasoningDelta]++
		case *responses.TextDeltaEvent:
			report.textDeltas = append(report.textDeltas, ev.Delta)
			report.handledEvents[responses.EventOutputTextDelta]++
		case *responses.FuncArgsDeltaEvent:
			report.handledEvents[responses.EventFuncArgsDelta]++
		case *responses.OutputItemDoneEvent:
			report.handledEvents[responses.EventOutputItemDone]++
		case *responses.ToolCompleteEvent:
			report.toolCalls = append(report.toolCalls, ev)
			// ToolCompleteEvent is synthesized from response.output_item.done.
			report.handledEvents[responses.EventOutputItemDone]++
		case *responses.ResponseCompletedEvent:
			report.completedEvt = ev
			if ev.Response.Status == responses.StatusFailed {
				report.handledEvents[responses.EventResponseFailed]++
			} else {
				report.handledEvents[responses.EventResponseCompleted]++
			}
		}
	}

	return report
}

func logUnhandledRawEvents(t *testing.T, rawCounts, handledCounts map[string]int) {
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

		if _, ok := knownNoOpEvents[name]; ok {
			t.Logf("observed known no-op raw event: %s x%d", name, remaining)
			continue
		}

		t.Logf("observed UNKNOWN unhandled raw event: %s x%d", name, remaining)
		unknown = append(unknown, name)
	}

	if os.Getenv("RESPONSES_STRICT_EVENTS") == "1" {
		require.Empty(t, unknown, "strict mode: unknown unhandled raw events detected")
	}
}

func TestIntegration_OpenRouter_TextResponse(t *testing.T) {
	apiKey := requireOpenRouterAPIKey(t)
	model := envOrDefault("OPENROUTER_RESPONSES_FREE_MODEL", "google/gemma-3-27b-it:free")

	rawCounts := map[string]int{}
	client := newOpenRouterClient(apiKey, func(name string) { rawCounts[name]++ })

	req := &responses.Request{
		Model:           model,
		Stream:          true,
		MaxOutputTokens: 16,
		Input: []responses.Input{
			{Role: "user", Content: "Reply with the single word: pong"},
		},
	}

	report := streamAndCollect(t, client, req)
	report.rawEvents = rawCounts

	require.Empty(t, report.streamErrs)
	require.NotNil(t, report.createdEvt)
	assert.NotEmpty(t, report.createdEvt.Response.ID)

	fullText := strings.Join(report.textDeltas, "")
	assert.NotEmpty(t, fullText)

	require.NotNil(t, report.completedEvt)
	assert.Equal(t, responses.StatusCompleted, report.completedEvt.Response.Status)

	if u := report.completedEvt.Response.Usage; u != nil {
		assert.Positive(t, u.InputTokens)
		assert.Positive(t, u.OutputTokens)
	}

	assert.Empty(t, report.toolCalls)

	logUnhandledRawEvents(t, report.rawEvents, report.handledEvents)
}

func TestIntegration_OpenRouter_ToolCall(t *testing.T) {
	apiKey := requireOpenRouterAPIKey(t)
	model := envOrDefault("OPENROUTER_RESPONSES_TOOL_MODEL", "meta-llama/llama-3.3-70b-instruct:free")

	rawCounts := map[string]int{}
	client := newOpenRouterClient(apiKey, func(name string) { rawCounts[name]++ })

	req := &responses.Request{
		Model:           model,
		Stream:          true,
		MaxOutputTokens: 64,
		Tools: []responses.Tool{{
			Type:        "function",
			Name:        "get_temperature",
			Description: "Get the current temperature for a city",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "City name"},
				},
				"required": []string{"city"},
			},
		}},
		ToolChoice: "required",
		Input: []responses.Input{
			{Role: "user", Content: "What is the temperature in Berlin?"},
		},
	}

	report := streamAndCollect(t, client, req)
	report.rawEvents = rawCounts

	require.Empty(t, report.streamErrs)
	require.NotEmpty(t, report.toolCalls)

	tc := report.toolCalls[0]
	assert.Equal(t, "get_temperature", tc.Name)
	assert.NotEmpty(t, tc.ID)
	city, _ := tc.Args["city"].(string)
	assert.NotEmpty(t, city)

	logUnhandledRawEvents(t, report.rawEvents, report.handledEvents)
}
