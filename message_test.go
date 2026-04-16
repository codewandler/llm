package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

// --- System Tests ---

func TestSystemMsg_MarshalJSON(t *testing.T) {
	m := msg.System("You are a helpful assistant.").Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "system", result["role"])
}

func TestSystemMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     msg.Message
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     msg.System("You are helpful.").Build(),
			wantErr: false,
		},
		{
			name:    "empty content",
			msg:     msg.System("").Build(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- UserMsg Tests ---

func TestUserMsg_MarshalJSON(t *testing.T) {
	m := msg.User("Hello, world!").Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "user", result["role"])
}

func TestUserMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     msg.Message
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     msg.User("Hello").Build(),
			wantErr: false,
		},
		{
			name:    "empty content",
			msg:     msg.User("").Build(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Assistant Tests ---

func TestAssistantMsg_MarshalJSON_ContentOnly(t *testing.T) {
	m := msg.Assistant(msg.Text("Hello! How can I help?")).Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
}

func TestAssistantMsg_MarshalJSON_WithPhase(t *testing.T) {
	m := msg.Assistant(msg.Text("Working"))
	m.Phase(msg.AssistantPhaseCommentary)

	data, err := json.Marshal(m.Build())
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "commentary", result["phase"])
}

func TestAssistantMsg_MarshalJSON_ToolCallsOnly(t *testing.T) {
	m := msg.Assistant(
		msg.ToolCall(msg.NewToolCall("call_123", "get_weather", msg.ToolArgs{"location": "Paris"})),
	).Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	parts, ok := result["parts"].([]any)
	require.True(t, ok)
	require.Len(t, parts, 1)

	part, ok := parts[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_call", part["type"])

	tc, ok := part["tool_call"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "call_123", tc["id"])
	assert.Equal(t, "get_weather", tc["name"])

	args := tc["args"].(map[string]any)
	assert.Equal(t, "Paris", args["location"])
}

func TestAssistantMsg_MarshalJSON_ContentAndToolCalls(t *testing.T) {
	m := msg.Assistant(
		msg.Text("Let me check the weather for you."),
		msg.ToolCall(msg.NewToolCall("call_456", "get_weather", msg.ToolArgs{"location": "London"})),
	).Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	parts, ok := result["parts"].([]any)
	require.True(t, ok)
	require.Len(t, parts, 2)

	// First part is text
	textPart := parts[0].(map[string]any)
	assert.Equal(t, "text", textPart["type"])
	assert.Equal(t, "Let me check the weather for you.", textPart["text"])

	// Second part is tool_call
	tcPart := parts[1].(map[string]any)
	assert.Equal(t, "tool_call", tcPart["type"])
}

func TestAssistantMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     msg.Message
		wantErr bool
	}{
		{
			name:    "content only",
			msg:     msg.Assistant(msg.Text("Hello")).Build(),
			wantErr: false,
		},
		{
			name: "tool calls only",
			msg: msg.Assistant(
				msg.ToolCall(msg.NewToolCall("1", "test", nil)),
			).Build(),
			wantErr: false,
		},
		{
			name: "content and tool calls",
			msg: msg.Assistant(
				msg.Text("Here's the result"),
				msg.ToolCall(msg.NewToolCall("1", "test", nil)),
			).Build(),
			wantErr: false,
		},
		{
			name:    "empty - no content or tool calls",
			msg:     msg.Assistant().Build(),
			wantErr: true,
		},
		{
			name:    "assistant phase allowed",
			msg:     msg.Assistant(msg.Text("Working")).Phase(msg.AssistantPhaseCommentary).Build(),
			wantErr: false,
		},
		{
			name:    "invalid assistant phase rejected",
			msg:     msg.Assistant(msg.Text("Working")).Phase(msg.AssistantPhase("bogus")).Build(),
			wantErr: true,
		},
		{
			name:    "non assistant phase rejected",
			msg:     msg.User("Hello").Phase(msg.AssistantPhaseCommentary).Build(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- ToolResult Tests ---

func TestToolCallResult_MarshalJSON(t *testing.T) {
	m := msg.Tool().Results(msg.ToolResult{
		ToolCallID: "call_123",
		ToolOutput: `{"temperature": 22}`,
	}).Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "tool", result["role"])
	parts, ok := result["parts"].([]any)
	require.True(t, ok)
	require.Len(t, parts, 1)

	part, ok := parts[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "tool_result", part["type"])

	tr, ok := part["tool_result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "call_123", tr["tool_call_id"])
	assert.Equal(t, `{"temperature": 22}`, tr["output"])
}

func TestToolCallResult_MarshalJSON_WithError(t *testing.T) {
	m := msg.Tool().Results(msg.ToolResult{
		ToolCallID: "call_456",
		ToolOutput: "Error: file not found",
		IsError:    true,
	}).Build()

	data, err := json.Marshal(m)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "tool", result["role"])
	parts := result["parts"].([]any)
	part := parts[0].(map[string]any)
	tr := part["tool_result"].(map[string]any)
	assert.Equal(t, "call_456", tr["tool_call_id"])
	assert.Equal(t, "Error: file not found", tr["output"])
	assert.Equal(t, true, tr["is_error"])
}

func TestToolCallResult_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     msg.Message
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     msg.Tool().Results(msg.ToolResult{ToolCallID: "123", ToolOutput: "result"}).Build(),
			wantErr: false,
		},
		{
			name:    "valid with error flag",
			msg:     msg.Tool().Results(msg.ToolResult{ToolCallID: "123", ToolOutput: "error", IsError: true}).Build(),
			wantErr: false,
		},
		{
			name:    "missing tool call id",
			msg:     msg.Tool().Results(msg.ToolResult{ToolOutput: "result"}).Build(),
			wantErr: true,
		},
		{
			name:    "missing output",
			msg:     msg.Tool().Results(msg.ToolResult{ToolCallID: "123"}).Build(),
			wantErr: true,
		},
		{
			name:    "empty",
			msg:     msg.Tool().Empty().Build(),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- tool.Call Tests ---

func TestToolCall_MarshalJSON(t *testing.T) {
	tc := tool.NewToolCall("call_abc", "search", map[string]any{"query": "golang", "limit": float64(10)})

	data, err := json.Marshal(tc)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "call_abc", result["id"])
	assert.Equal(t, "search", result["name"])

	args := result["args"].(map[string]any)
	assert.Equal(t, "golang", args["query"])
	assert.Equal(t, float64(10), args["limit"])
}

func TestToolCall_UnmarshalJSON(t *testing.T) {
	tc := tool.NewToolCall("call_xyz", "get_weather", map[string]any{"location": "Paris", "unit": "celsius"})

	assert.Equal(t, "call_xyz", tc.ToolCallID())
	assert.Equal(t, "get_weather", tc.ToolName())
	assert.Equal(t, "Paris", tc.ToolArgs()["location"])
	assert.Equal(t, "celsius", tc.ToolArgs()["unit"])
}

func TestToolCall_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := tool.NewToolCall("call_roundtrip", "complex_tool", map[string]any{"nested": map[string]any{"key": "value"}, "array": []any{1.0, 2.0, 3.0}})

	data, err := json.Marshal(original)
	require.NoError(t, err)

	assert.Contains(t, string(data), "call_roundtrip")
	assert.Contains(t, string(data), "complex_tool")
}

func TestToolCall_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tc      tool.Call
		wantErr bool
	}{
		{
			name:    "valid",
			tc:      tool.NewToolCall("123", "test", nil),
			wantErr: false,
		},
		{
			name:    "valid with arguments",
			tc:      tool.NewToolCall("123", "test", map[string]any{"key": "value"}),
			wantErr: false,
		},
		{
			name:    "missing id",
			tc:      tool.NewToolCall("", "test", nil),
			wantErr: true,
		},
		{
			name:    "missing name",
			tc:      tool.NewToolCall("123", "", nil),
			wantErr: true,
		},
		{
			name:    "empty",
			tc:      tool.NewToolCall("", "", nil),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tc.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Messages Tests ---

func TestMessages_UnmarshalJSON_SingleMessage(t *testing.T) {
	jsonData := `[{"role": "user", "content": "Hello"}]`

	var msgs msg.Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)
	assert.Equal(t, msg.RoleUser, msgs[0].Role)
}

func TestMessages_UnmarshalJSON_AllMessageTypes(t *testing.T) {
	jsonData := `[
		{"role": "system", "content": "You are helpful."},
		{"role": "user", "content": "What's the weather?"},
		{"role": "assistant", "tool_calls": [{"id": "call_1", "name": "get_weather", "args": {"location": "Paris"}}]},
		{"role": "tool", "tool_call_id": "call_1", "content": "{\"temp\": 22}", "is_error": false}
	]`

	var msgs msg.Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 4)

	// Check System
	assert.Equal(t, msg.RoleSystem, msgs[0].Role)

	// Check UserMsg
	assert.Equal(t, msg.RoleUser, msgs[1].Role)

	// Check Assistant with ToolCalls
	assert.Equal(t, msg.RoleAssistant, msgs[2].Role)

	// Check ToolResult
	assert.Equal(t, msg.RoleTool, msgs[3].Role)
}

func TestMessages_UnmarshalJSON_AssistantWithContent(t *testing.T) {
	jsonData := `[{"role": "assistant", "content": "Hello there!"}]`

	var msgs msg.Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)
	assert.Equal(t, msg.RoleAssistant, msgs[0].Role)
}

func TestMessages_UnmarshalJSON_AssistantWithContentAndToolCalls(t *testing.T) {
	jsonData := `[{"role": "assistant", "content": "Let me help.", "tool_calls": [{"id": "1", "name": "test", "arguments": {}}]}]`

	var msgs msg.Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)
	assert.Equal(t, msg.RoleAssistant, msgs[0].Role)
}

func TestMessages_UnmarshalJSON_ToolResultWithError(t *testing.T) {
	jsonData := `[{"role": "tool", "tool_call_id": "call_err", "content": "Something went wrong", "is_error": true}]`

	var msgs msg.Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)
	assert.Equal(t, msg.RoleTool, msgs[0].Role)
}

// --- Transcript Tests ---

func TestBuildTranscript(t *testing.T) {
	transcript := msg.BuildTranscript(
		System("You are helpful."),
		User("What is 2+2?"),
		Assistant("It is 4."),
		msg.Assistant(msg.ToolCall(msg.NewToolCall("tc1", "calc", msg.ToolArgs{"expr": "2+2"}))),
		msg.Tool().Results(msg.ToolResult{ToolCallID: "tc1", ToolOutput: "4"}),
	)

	require.Len(t, transcript, 5)
	assert.Equal(t, RoleSystem, transcript[0].Role)
	assert.Equal(t, RoleUser, transcript[1].Role)
	assert.Equal(t, RoleAssistant, transcript[2].Role)
	assert.Equal(t, RoleAssistant, transcript[3].Role)
	assert.Equal(t, RoleTool, transcript[4].Role)
}

func TestBuildTranscript_NestedAssistant(t *testing.T) {
	transcript := msg.BuildTranscript(
		msg.Assistant(
			msg.Text("Let me calculate that."),
			msg.ToolCall(msg.NewToolCall("tc1", "calc", msg.ToolArgs{"expr": "2+2"})),
		),
		msg.Tool().Results(msg.ToolResult{ToolCallID: "tc1", ToolOutput: "4"}),
		msg.Assistant(msg.Text("The answer is 4.")),
	)

	require.Len(t, transcript, 3)
	assert.Equal(t, RoleAssistant, transcript[0].Role)
	assert.Equal(t, RoleTool, transcript[1].Role)
	assert.Equal(t, RoleAssistant, transcript[2].Role)
}

// --- Role Tests ---

func TestMessage_RoleGetters(t *testing.T) {
	tests := []struct {
		name   string
		msg    Message
		system bool
		user   bool
		asst   bool
		tool   bool
		dev    bool
	}{
		{"system", System("hello"), true, false, false, false, false},
		{"user", User("hello"), false, true, false, false, false},
		{"assistant", Assistant("hello"), false, false, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.system, tt.msg.IsSystem())
			assert.Equal(t, tt.user, tt.msg.IsUser())
			assert.Equal(t, tt.asst, tt.msg.IsAssistant())
			assert.Equal(t, tt.tool, tt.msg.IsTool())
			assert.Equal(t, tt.dev, tt.msg.IsDeveloper())
		})
	}
}

// --- Tool Call Result Tests ---

func TestToolCallResult_ToolOutput(t *testing.T) {
	result := msg.ToolResult{
		ToolCallID: "call_123",
		ToolOutput: "test output",
		IsError:    false,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "call_123", parsed["tool_call_id"])
	assert.Equal(t, "test output", parsed["output"])
	assert.Nil(t, parsed["is_error"]) // omitempty, not present when false
}
