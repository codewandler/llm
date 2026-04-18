//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

type matrixScenario struct {
	name    string
	request func(model string) llm.Request
	enabled func(provider matrixProvider) (bool, string)
	assert  func(t *testing.T, run matrixRun)
}

func matrixScenarios() []matrixScenario {
	return []matrixScenario{
		{
			name: "plain_text_pong",
			request: func(model string) llm.Request {
				return llm.Request{
					Model:     model,
					MaxTokens: 64,
					Thinking:  llm.ThinkingOff,
					Messages: msg.BuildTranscript(
						msg.User("Reply with pong."),
					),
				}
			},
			enabled: alwaysEnabled,
			assert:  assertTextContains("pong"),
		},
		{
			name: "system_prompt_kiwi",
			request: func(model string) llm.Request {
				return llm.Request{
					Model:     model,
					MaxTokens: 64,
					Thinking:  llm.ThinkingOff,
					Messages: msg.BuildTranscript(
						msg.System("Reply with exactly the word kiwi."),
						msg.User("What should you reply with?"),
					),
				}
			},
			enabled: alwaysEnabled,
			assert:  assertTextContains("kiwi"),
		},
		{
			name: "effort_high_thinking_off",
			request: func(model string) llm.Request {
				return llm.Request{
					Model:    model,
					Thinking: llm.ThinkingOff,
					Effort:   llm.EffortHigh,
					Messages: msg.BuildTranscript(
						msg.User("Reply with exactly the word aurora."),
					),
				}
			},
			enabled: requiresEffortSupport,
			assert:  assertEffortPreserved("aurora", llm.EffortHigh),
		},
		{
			name: "thinking_text_comet",
			request: func(model string) llm.Request {
				return llm.Request{
					Model:     model,
					MaxTokens: 256,
					Thinking:  llm.ThinkingOn,
					Effort:    llm.EffortHigh,
					Messages: msg.BuildTranscript(
						msg.System("If reasoning is available, use it briefly before the final answer."),
						msg.User("Reply with the final word comet."),
					),
				}
			},
			enabled: requiresReasoningSupport,
			assert:  assertReasoningScenario("comet"),
		},
	}
}

func alwaysEnabled(provider matrixProvider) (bool, string) {
	return true, ""
}

func requiresReasoningSupport(provider matrixProvider) (bool, string) {
	if provider.supportsReasoning == nil {
		return false, "provider does not advertise reasoning support"
	}
	req := llm.Request{Model: provider.model, Thinking: llm.ThinkingOn, Effort: llm.EffortHigh}
	if !provider.supportsReasoning(req) {
		return false, "provider/model does not expose reasoning for this scenario"
	}
	return true, ""
}

func assertTextContains(want string) func(t *testing.T, run matrixRun) {
	want = strings.ToLower(want)

	return func(t *testing.T, run matrixRun) {
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

func requiresEffortSupport(provider matrixProvider) (bool, string) {
	if provider.supportsEffort == nil {
		return false, "provider does not advertise effort support"
	}
	req := llm.Request{Model: provider.model, Effort: llm.EffortHigh, Thinking: llm.ThinkingOff}
	if !provider.supportsEffort(req) {
		return false, "provider/model does not support effort control"
	}
	return true, ""
}

func assertEffortPreserved(wantText string, wantEffort llm.Effort) func(t *testing.T, run matrixRun) {
	textAssert := assertTextContains(wantText)

	return func(t *testing.T, run matrixRun) {
		t.Helper()

		textAssert(t, run)

		// Verify the wire request actually contains the reasoning effort.
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
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatalf("unmarshal wire body: %v", err)
		}
		if wire.Reasoning == nil {
			t.Fatalf("expected reasoning field in wire request body, got: %s", string(body))
		}
		if wire.Reasoning.Effort == "" {
			t.Fatalf("expected reasoning.effort to be set in wire request, got empty; body: %s", string(body))
		}

		// The wire effort may be a provider-specific mapping (e.g. "max" → "xhigh"),
		// so we only verify it is non-empty, not the exact string. But it must not be
		// a lower effort than requested (sanity check for the common case).
		effortOrder := map[string]int{"low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 4}
		gotRank := effortOrder[wire.Reasoning.Effort]
		wantRank := effortOrder[string(wantEffort)]
		if gotRank < wantRank {
			t.Fatalf("wire reasoning.effort = %q (rank %d) is lower than requested %q (rank %d)",
				wire.Reasoning.Effort, gotRank, wantEffort, wantRank)
		}

		t.Logf("wire reasoning.effort = %q (requested %q)", wire.Reasoning.Effort, wantEffort)
	}
}

func assertReasoningScenario(want string) func(t *testing.T, run matrixRun) {
	textAssert := assertTextContains(want)

	return func(t *testing.T, run matrixRun) {
		t.Helper()

		textAssert(t, run)
		if run.provider.supportsReasoning == nil || !run.provider.supportsReasoning(run.request) {
			return
		}
		if run.reasoningStreamCount() == 0 && strings.TrimSpace(run.result.Thought()) == "" {
			t.Fatalf("expected reasoning events for %s, got %s", run.provider.name, run.streamSummary())
		}
	}
}
