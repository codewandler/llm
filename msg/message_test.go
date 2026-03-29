package msg

import (
	"encoding/json"
	"testing"
)

func TestBuild(t *testing.T) {
	transcript := BuildTranscript(
		System("you are nice").Cache(),
		User("hey, what is 1+1"),
		Assistant(
			Text("I am using my calculator"),
			Thinking("I should check my tools", ""),
			ToolCall{
				ID:   "toolcall_1",
				Name: "calculator",
				Args: ToolArgs{"expression": "1+1"},
			},
		),
		ToolResult{
			ToolCallID: "toolcall_1",
			ToolOutput: "2",
			IsError:    false,
		},
		User("Thanks, whats the capital of France?"),
		Assistant(Text("Paris")),
		User("Whats the temperature in Paris?"),
		Assistant(
			Text("Checking api ..."),
			ToolCall{ID: "toolcall_2", Name: "weather", Args: ToolArgs{"location": "Paris"}},
		),
		BuildFrom(ToolResult{
			ToolCallID: "toolcall_2",
			ToolOutput: `{"temperature": 22, "conditions": "sunny"}`,
			IsError:    false,
		}).Cache(),
		Assistant(Text("The temperature is 22 degrees and sunny")),
	)

	d, _ := json.MarshalIndent(transcript, "", "  ")
	t.Log(string(d))
}
