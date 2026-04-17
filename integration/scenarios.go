//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

type matrixScenario struct {
	name    string
	request func(model string) llm.Request
	assert  func(t *testing.T, run matrixRun)
}

func matrixScenarios() []matrixScenario {
	return []matrixScenario{
		{
			name: "plain_text_pong",
			request: func(model string) llm.Request {
				return llm.Request{
					Model:     model,
					MaxTokens: 32,
					Messages: msg.BuildTranscript(
						msg.User("Reply with exactly the word pong."),
					),
				}
			},
			assert: assertTextContains("pong"),
		},
		{
			name: "system_prompt_kiwi",
			request: func(model string) llm.Request {
				return llm.Request{
					Model:     model,
					MaxTokens: 32,
					Messages: msg.BuildTranscript(
						msg.System("Reply with exactly the word kiwi."),
						msg.User("What should you reply with?"),
					),
				}
			},
			assert: assertTextContains("kiwi"),
		},
	}
}

func assertTextContains(want string) func(t *testing.T, run matrixRun) {
	want = strings.ToLower(want)

	return func(t *testing.T, run matrixRun) {
		t.Helper()

		if run.deltaCount == 0 {
			t.Fatalf("expected at least one delta event, got event types %s", run.eventTypesString())
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
