//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

const matrixScenarioTimeout = 90 * time.Second

type matrixRun struct {
	provider       matrixProvider
	scenario       matrixScenario
	request        llm.Request
	envelopes      []llm.Envelope
	requestEvent   *llm.RequestEvent
	startedEvent   *llm.StreamStartedEvent
	completedEvent *llm.CompletedEvent
	deltaCount     int
	usageCount     int
	contentParts   int
	toolCallCount  int
	result         llm.Result
}

func executeMatrixScenario(t *testing.T, provider matrixProvider, scenario matrixScenario) matrixRun {
	t.Helper()

	run := matrixRun{
		provider: provider,
		scenario: scenario,
		request:  scenario.request(provider.model),
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
				run.deltaCount++
			}
		case llm.StreamEventUsageUpdated:
			if _, ok := env.Data.(*llm.UsageUpdatedEvent); !ok {
				t.Fatalf("usage event payload = %T, want *llm.UsageUpdatedEvent", env.Data)
			}
			run.usageCount++
		case llm.StreamEventContentPart:
			if _, ok := env.Data.(*llm.ContentPartEvent); !ok {
				t.Fatalf("content part payload = %T, want *llm.ContentPartEvent", env.Data)
			}
			run.contentParts++
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
		t.Fatalf("expected ProcessEvents() to produce text, got empty result (event types %s)", run.eventTypesString())
	}
	if run.result.StopReason() != run.completedEvent.StopReason {
		t.Fatalf("ProcessEvents() stop reason = %q, completed event stop reason = %q", run.result.StopReason(), run.completedEvent.StopReason)
	}
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
