//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

type integrationScenario struct {
	name    string
	request func(model string) llm.Request
	enabled func(target integrationTarget) (bool, string)
	assert  func(t *testing.T, run integrationRun)
}

func integrationScenarios() []integrationScenario {
	return []integrationScenario{
		{
			name: "plain_text_pong",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 64, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.User("Reply with pong."))}
			},
			enabled: alwaysEnabled,
			assert:  assertTextContains("pong"),
		},
		{
			name: "system_prompt_kiwi",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 64, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.System("Reply with exactly the word kiwi."), msg.User("What should you reply with?"))}
			},
			enabled: alwaysEnabled,
			assert:  assertTextContains("kiwi"),
		},
		{
			name: "effort_high_preserved",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, Effort: llm.EffortHigh, Messages: msg.BuildTranscript(msg.User("Reply with exactly the word aurora."))}
			},
			enabled: requiresEffortSupport,
			assert:  assertEffortPreserved("aurora", llm.EffortHigh),
		},
		{
			name: "thinking_off_respected",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.User("Reply with exactly the word ember."))}
			},
			enabled: requiresThinkingToggleSupport,
			assert:  assertThinkingOffRespected("ember"),
		},
		{
			name: "thinking_text_comet",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 256, Thinking: llm.ThinkingOn, Effort: llm.EffortHigh, Messages: msg.BuildTranscript(msg.System("If reasoning is available, use it briefly before the final answer."), msg.User("Reply with the final word comet."))}
			},
			enabled: requiresReasoningSupport,
			assert:  assertReasoningScenario("comet"),
		},
	}
}

func alwaysEnabled(target integrationTarget) (bool, string) { return true, "" }
func requiresReasoningSupport(target integrationTarget) (bool, string) {
	if !target.supports.Reasoning {
		return false, "target does not advertise reasoning support"
	}
	return true, ""
}
func requiresThinkingToggleSupport(target integrationTarget) (bool, string) {
	if !target.supports.ThinkingToggle {
		return false, "target does not advertise thinking toggle support"
	}
	return true, ""
}
func requiresEffortSupport(target integrationTarget) (bool, string) {
	if !target.supports.Effort {
		return false, "target does not advertise effort support"
	}
	return true, ""
}

func assertTextContains(want string) func(t *testing.T, run integrationRun) {
	want = strings.ToLower(want)
	return func(t *testing.T, run integrationRun) {
		t.Helper()
		if run.textStreamCount() == 0 {
			t.Fatalf("expected streamed text events, got %s", run.streamSummary())
		}
		text := strings.ToLower(run.result.Text())
		if !strings.Contains(text, want) {
			t.Fatalf("expected processed text to contain %q, got %q (event types %s)", want, run.result.Text(), run.eventTypesString())
		}
		message := run.result.Message()
		if message.Role != msg.RoleAssistant {
			t.Fatalf("processed message role = %q, want %q", message.Role, msg.RoleAssistant)
		}
		if !strings.Contains(strings.ToLower(message.Text()), want) {
			t.Fatalf("expected processed assistant message to contain %q, got %q", want, message.Text())
		}
	}
}

func assertEffortPreserved(wantText string, wantEffort llm.Effort) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(wantText)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if run.requestEvent == nil {
			t.Fatal("expected a request event to inspect wire body")
		}
		body := run.requestEvent.ProviderRequest.Body
		if len(body) == 0 {
			t.Fatal("expected non-empty provider request body")
		}
		var wire struct {
			Reasoning *struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			OutputConfig *struct {
				Effort string `json:"effort"`
			} `json:"output_config"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatalf("unmarshal wire body: %v", err)
		}
		gotEffort := ""
		if wire.Reasoning != nil && wire.Reasoning.Effort != "" {
			gotEffort = wire.Reasoning.Effort
		}
		if gotEffort == "" && wire.OutputConfig != nil && wire.OutputConfig.Effort != "" {
			gotEffort = wire.OutputConfig.Effort
		}
		if gotEffort == "" {
			t.Fatalf("expected effort in wire request body, got: %s", string(body))
		}
		effortOrder := map[string]int{"low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 4}
		if effortOrder[gotEffort] < effortOrder[string(wantEffort)] {
			t.Fatalf("wire effort = %q is lower than requested %q", gotEffort, wantEffort)
		}
	}
}

func assertReasoningScenario(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if !run.target.supports.Reasoning {
			return
		}
		if run.reasoningStreamCount() == 0 && strings.TrimSpace(run.result.Thought()) == "" {
			t.Fatalf("expected reasoning events for %s, got %s", run.target.name, run.streamSummary())
		}
	}
}

func assertThinkingOffRespected(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if run.reasoningStreamCount() > 0 || strings.TrimSpace(run.result.Thought()) != "" {
			t.Fatalf("expected no reasoning output when thinking is disabled, got %s", run.streamSummary())
		}
	}
}
