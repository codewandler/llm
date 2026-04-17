//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	agentunified "github.com/codewandler/agentapis/api/unified"
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

const matrixScenarioTimeout = 90 * time.Second

type matrixRun struct {
	provider                  matrixProvider
	scenario                  matrixScenario
	request                   llm.Request
	envelopes                 []llm.Envelope
	requestEvent              *llm.RequestEvent
	startedEvent              *llm.StreamStartedEvent
	completedEvent            *llm.CompletedEvent
	textDeltaCount            int
	reasoningDeltaCount       int
	usageCount                int
	contentParts              int
	textContentPartCount      int
	reasoningContentPartCount int
	toolCallCount             int
	debugSummaries            []string
	result                    llm.Result
}

func executeMatrixScenario(t *testing.T, provider matrixProvider, scenario matrixScenario) matrixRun {
	t.Helper()

	run := matrixRun{
		provider: provider,
		scenario: scenario,
		request:  scenario.request(provider.model),
	}
	if provider.prepareRequest != nil {
		run.request = provider.prepareRequest(run.request)
	}

	p, err := provider.newProvider()
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), matrixScenarioTimeout)
	defer cancel()

	stream, err := p.CreateStream(ctx, run.request)
	if err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}

	for env := range stream {
		run.envelopes = append(run.envelopes, env)

		switch env.Type {
		case llm.StreamEventRequest:
			reqEv, ok := env.Data.(*llm.RequestEvent)
			if !ok {
				t.Fatalf("request event payload = %T, want *llm.RequestEvent", env.Data)
			}
			run.requestEvent = reqEv
		case llm.StreamEventStarted:
			startedEv, ok := env.Data.(*llm.StreamStartedEvent)
			if !ok {
				t.Fatalf("started event payload = %T, want *llm.StreamStartedEvent", env.Data)
			}
			run.startedEvent = startedEv
		case llm.StreamEventDelta:
			deltaEv, ok := env.Data.(*llm.DeltaEvent)
			if !ok {
				t.Fatalf("delta event payload = %T, want *llm.DeltaEvent", env.Data)
			}
			if deltaEv.Kind == llm.DeltaKindText {
				run.textDeltaCount++
			}
			if deltaEv.Kind == llm.DeltaKindThinking {
				run.reasoningDeltaCount++
			}
		case llm.StreamEventUsageUpdated:
			if _, ok := env.Data.(*llm.UsageUpdatedEvent); !ok {
				t.Fatalf("usage event payload = %T, want *llm.UsageUpdatedEvent", env.Data)
			}
			run.usageCount++
		case llm.StreamEventContentPart:
			contentEv, ok := env.Data.(*llm.ContentPartEvent)
			if !ok {
				t.Fatalf("content part payload = %T, want *llm.ContentPartEvent", env.Data)
			}
			run.contentParts++
			switch contentEv.Part.Type {
			case msg.PartTypeText:
				run.textContentPartCount++
			case msg.PartTypeThinking:
				run.reasoningContentPartCount++
			}
		case llm.StreamEventToolCall:
			if _, ok := env.Data.(*llm.ToolCallEvent); !ok {
				t.Fatalf("tool call payload = %T, want *llm.ToolCallEvent", env.Data)
			}
			run.toolCallCount++
		case llm.StreamEventCompleted:
			completedEv, ok := env.Data.(*llm.CompletedEvent)
			if !ok {
				t.Fatalf("completed event payload = %T, want *llm.CompletedEvent", env.Data)
			}
			run.completedEvent = completedEv
		case llm.StreamEventDebug:
			debugEv, ok := env.Data.(*llm.DebugEvent)
			if !ok {
				t.Fatalf("debug event payload = %T, want *llm.DebugEvent", env.Data)
			}
			run.debugSummaries = append(run.debugSummaries, summarizeDebugEvent(debugEv))
		case llm.StreamEventError:
			errEv, ok := env.Data.(*llm.ErrorEvent)
			if !ok {
				t.Fatalf("error event payload = %T, want *llm.ErrorEvent", env.Data)
			}
			t.Fatalf("unexpected stream error: %v (event types %s)", errEv.Error, run.eventTypesString())
		}
	}

	run.result = replayProcessedResult(run.envelopes)
	assertMatrixBase(t, run)
	return run
}

func replayProcessedResult(envelopes []llm.Envelope) llm.Result {
	replay := make(chan llm.Envelope, len(envelopes))
	for _, env := range envelopes {
		replay <- env
	}
	close(replay)
	return llm.ProcessEvents(context.Background(), replay)
}

func assertMatrixBase(t *testing.T, run matrixRun) {
	t.Helper()

	if len(run.envelopes) == 0 {
		t.Fatal("expected at least one event envelope")
	}
	if run.requestEvent == nil {
		t.Fatalf("expected a request event, got event types %s", run.eventTypesString())
	}
	if run.startedEvent == nil {
		t.Fatalf("expected a started event, got event types %s", run.eventTypesString())
	}
	if run.completedEvent == nil {
		t.Fatalf("expected a completed event, got event types %s", run.eventTypesString())
	}

	wantAPI := run.provider.expectedAPIType(run.request)
	if run.requestEvent.ResolvedApiType != wantAPI {
		t.Fatalf("ResolvedApiType = %q, want %q", run.requestEvent.ResolvedApiType, wantAPI)
	}

	if run.requestEvent.ProviderRequest.Method == "" {
		t.Fatal("expected provider request method on request event")
	}
	if run.requestEvent.ProviderRequest.URL == "" {
		t.Fatal("expected provider request URL on request event")
	}

	if err := run.result.Error(); err != nil {
		t.Fatalf("ProcessEvents() error = %v", err)
	}
	if run.result.StopReason() == llm.StopReasonUnknown {
		t.Fatalf("expected a non-empty stop reason, got %q", run.result.StopReason())
	}
	if run.result.Message().Role != msg.RoleAssistant {
		t.Fatalf("processed message role = %q, want %q", run.result.Message().Role, msg.RoleAssistant)
	}
	if len(run.result.Next()) == 0 {
		t.Fatal("expected ProcessEvents() to produce a next transcript")
	}
	if run.result.Text() == "" {
		t.Fatalf("expected ProcessEvents() to produce text, got empty result (%s)", run.streamSummary())
	}
	if run.result.StopReason() != run.completedEvent.StopReason {
		t.Fatalf("ProcessEvents() stop reason = %q, completed event stop reason = %q", run.result.StopReason(), run.completedEvent.StopReason)
	}
}

func (r matrixRun) textStreamCount() int {
	return r.textDeltaCount + r.textContentPartCount
}

func (r matrixRun) reasoningStreamCount() int {
	return r.reasoningDeltaCount + r.reasoningContentPartCount
}

func (r matrixRun) streamSummary() string {
	if len(r.debugSummaries) == 0 {
		return fmt.Sprintf("event types %s, stop_reason=%q, text_deltas=%d, text_parts=%d, reasoning_deltas=%d, reasoning_parts=%d", r.eventTypesString(), r.completedStopReason(), r.textDeltaCount, r.textContentPartCount, r.reasoningDeltaCount, r.reasoningContentPartCount)
	}
	return fmt.Sprintf("event types %s, stop_reason=%q, text_deltas=%d, text_parts=%d, reasoning_deltas=%d, reasoning_parts=%d, debug=%s", r.eventTypesString(), r.completedStopReason(), r.textDeltaCount, r.textContentPartCount, r.reasoningDeltaCount, r.reasoningContentPartCount, strings.Join(r.debugSummaries, " | "))
}

func (r matrixRun) completedStopReason() llm.StopReason {
	if r.completedEvent == nil {
		return ""
	}
	return r.completedEvent.StopReason
}

func summarizeDebugEvent(debugEv *llm.DebugEvent) string {
	if debugEv == nil {
		return "<nil>"
	}
	if ev, ok := debugEv.Data.(agentunified.StreamEvent); ok {
		parts := []string{fmt.Sprintf("debug:%s type=%s", debugEv.Message, ev.Type)}
		if ev.ContentDelta != nil {
			parts = append(parts, fmt.Sprintf("content_delta(kind=%s data=%q)", ev.ContentDelta.Kind, ev.ContentDelta.Data))
		}
		if ev.StreamContent != nil {
			parts = append(parts, fmt.Sprintf("stream_content(kind=%s data=%q)", ev.StreamContent.Kind, ev.StreamContent.Data))
		}
		if ev.Content != nil {
			parts = append(parts, fmt.Sprintf("content_part(type=%s text=%q)", ev.Content.Part.Type, ev.Content.Part.Text))
		}
		if ev.Delta != nil {
			parts = append(parts, fmt.Sprintf("delta(kind=%s text=%q thinking=%q)", ev.Delta.Kind, ev.Delta.Text, ev.Delta.Thinking))
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprintf("debug:%s %T", debugEv.Message, debugEv.Data)
}

func (r matrixRun) eventTypesString() string {
	if len(r.envelopes) == 0 {
		return "<none>"
	}
	types := make([]string, 0, len(r.envelopes))
	for _, env := range r.envelopes {
		types = append(types, string(env.Type))
	}
	return fmt.Sprintf("[%s]", strings.Join(types, ", "))
}
