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

var knownNoOpEvents = map[string]struct{}{}

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
	toolCalls     []*responses.OutputItemDoneEvent
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

		if name := handledEventName(result.Event); name != "" {
			report.handledEvents[name]++
		}

		switch ev := result.Event.(type) {
		case *responses.ResponseCreatedEvent:
			report.createdEvt = ev
		case *responses.OutputTextDeltaEvent:
			report.textDeltas = append(report.textDeltas, ev.Delta)
		case *responses.ReasoningSummaryTextDeltaEvent, *responses.ReasoningTextDeltaEvent:
			report.reasoningSeen++
		case *responses.OutputItemDoneEvent:
			if ev.Item.Type == "function_call" {
				report.toolCalls = append(report.toolCalls, ev)
			}
		case *responses.ResponseCompletedEvent:
			report.completedEvt = ev
		}
	}

	return report
}

func handledEventName(ev any) string {
	switch ev.(type) {
	case *responses.ResponseCreatedEvent:
		return responses.EventResponseCreated
	case *responses.ResponseInProgressEvent:
		return responses.EventResponseInProgress
	case *responses.ResponseCompletedEvent:
		return responses.EventResponseCompleted
	case *responses.ResponseFailedEvent:
		return responses.EventResponseFailed
	case *responses.ResponseIncompleteEvent:
		return responses.EventResponseIncomplete
	case *responses.ResponseQueuedEvent:
		return responses.EventResponseQueued
	case *responses.OutputItemAddedEvent:
		return responses.EventOutputItemAdded
	case *responses.OutputItemDoneEvent:
		return responses.EventOutputItemDone
	case *responses.ContentPartAddedEvent:
		return responses.EventContentPartAdded
	case *responses.ContentPartDoneEvent:
		return responses.EventContentPartDone
	case *responses.OutputTextDeltaEvent:
		return responses.EventOutputTextDelta
	case *responses.OutputTextDoneEvent:
		return responses.EventOutputTextDone
	case *responses.OutputTextAnnotationAddedEvent:
		return responses.EventOutputTextAnnotationAdded
	case *responses.RefusalDeltaEvent:
		return responses.EventRefusalDelta
	case *responses.RefusalDoneEvent:
		return responses.EventRefusalDone
	case *responses.FunctionCallArgumentsDeltaEvent:
		return responses.EventFunctionCallArgumentsDelta
	case *responses.FunctionCallArgumentsDoneEvent:
		return responses.EventFunctionCallArgumentsDone
	case *responses.FileSearchCallInProgressEvent:
		return responses.EventFileSearchCallInProgress
	case *responses.FileSearchCallSearchingEvent:
		return responses.EventFileSearchCallSearching
	case *responses.FileSearchCallCompletedEvent:
		return responses.EventFileSearchCallCompleted
	case *responses.WebSearchCallInProgressEvent:
		return responses.EventWebSearchCallInProgress
	case *responses.WebSearchCallSearchingEvent:
		return responses.EventWebSearchCallSearching
	case *responses.WebSearchCallCompletedEvent:
		return responses.EventWebSearchCallCompleted
	case *responses.ReasoningSummaryPartAddedEvent:
		return responses.EventReasoningSummaryPartAdded
	case *responses.ReasoningSummaryPartDoneEvent:
		return responses.EventReasoningSummaryPartDone
	case *responses.ReasoningSummaryTextDeltaEvent:
		return responses.EventReasoningSummaryTextDelta
	case *responses.ReasoningSummaryTextDoneEvent:
		return responses.EventReasoningSummaryTextDone
	case *responses.ReasoningTextDeltaEvent:
		return responses.EventReasoningTextDelta
	case *responses.ReasoningTextDoneEvent:
		return responses.EventReasoningTextDone
	case *responses.ImageGenerationCallCompletedEvent:
		return responses.EventImageGenerationCallCompleted
	case *responses.ImageGenerationCallGeneratingEvent:
		return responses.EventImageGenerationCallGenerating
	case *responses.ImageGenerationCallInProgressEvent:
		return responses.EventImageGenerationCallInProgress
	case *responses.ImageGenerationCallPartialImageEvent:
		return responses.EventImageGenerationCallPartialImage
	case *responses.MCPCallArgumentsDeltaEvent:
		return responses.EventMCPCallArgumentsDelta
	case *responses.MCPCallArgumentsDoneEvent:
		return responses.EventMCPCallArgumentsDone
	case *responses.MCPCallCompletedEvent:
		return responses.EventMCPCallCompleted
	case *responses.MCPCallFailedEvent:
		return responses.EventMCPCallFailed
	case *responses.MCPCallInProgressEvent:
		return responses.EventMCPCallInProgress
	case *responses.MCPListToolsCompletedEvent:
		return responses.EventMCPListToolsCompleted
	case *responses.MCPListToolsFailedEvent:
		return responses.EventMCPListToolsFailed
	case *responses.MCPListToolsInProgressEvent:
		return responses.EventMCPListToolsInProgress
	case *responses.CodeInterpreterCallInProgressEvent:
		return responses.EventCodeInterpreterCallInProgress
	case *responses.CodeInterpreterCallInterpretingEvent:
		return responses.EventCodeInterpreterCallInterpreting
	case *responses.CodeInterpreterCallCompletedEvent:
		return responses.EventCodeInterpreterCallCompleted
	case *responses.CodeInterpreterCallCodeDeltaEvent:
		return responses.EventCodeInterpreterCallCodeDelta
	case *responses.CodeInterpreterCallCodeDoneEvent:
		return responses.EventCodeInterpreterCallCodeDone
	case *responses.CustomToolCallInputDeltaEvent:
		return responses.EventCustomToolCallInputDelta
	case *responses.CustomToolCallInputDoneEvent:
		return responses.EventCustomToolCallInputDone
	case *responses.AudioTranscriptDoneEvent:
		return responses.EventAudioTranscriptDone
	case *responses.AudioTranscriptDeltaEvent:
		return responses.EventAudioTranscriptDelta
	case *responses.AudioDoneEvent:
		return responses.EventAudioDone
	case *responses.AudioDeltaEvent:
		return responses.EventAudioDelta
	default:
		return ""
	}
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
		Input:           []responses.Input{{Role: "user", Content: "Reply with the single word: pong"}},
	}

	report := streamAndCollect(t, client, req)
	report.rawEvents = rawCounts

	require.Empty(t, report.streamErrs)
	require.NotNil(t, report.createdEvt)
	assert.NotEmpty(t, report.createdEvt.Response.ID)
	assert.NotEmpty(t, strings.Join(report.textDeltas, ""))

	require.NotNil(t, report.completedEvt)
	assert.Equal(t, responses.StatusCompleted, report.completedEvt.Response.Status)
	if u := report.completedEvt.Response.Usage; u != nil {
		assert.Positive(t, u.InputTokens)
		assert.GreaterOrEqual(t, u.OutputTokens, 0)
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
		Input:      []responses.Input{{Role: "user", Content: "What is the temperature in Berlin?"}},
	}

	report := streamAndCollect(t, client, req)
	report.rawEvents = rawCounts

	require.Empty(t, report.streamErrs)
	require.NotEmpty(t, report.toolCalls)

	tc := report.toolCalls[0]
	assert.Equal(t, "get_temperature", tc.Item.Name)
	assert.NotEmpty(t, tc.Item.CallID)
	assert.Contains(t, tc.Item.Arguments, "city")

	logUnhandledRawEvents(t, report.rawEvents, report.handledEvents)
}
