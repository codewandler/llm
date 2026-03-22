package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SystemMsg Tests ---

func TestSystemMsg_MarshalJSON(t *testing.T) {
	msg := &SystemMsg{Content: "You are a helpful assistant."}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "system", result["role"])
	assert.Equal(t, "You are a helpful assistant.", result["content"])
}

func TestSystemMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *SystemMsg
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     &SystemMsg{Content: "You are helpful."},
			wantErr: false,
		},
		{
			name:    "empty content",
			msg:     &SystemMsg{Content: ""},
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
	msg := &UserMsg{Content: "Hello, world!"}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "user", result["role"])
	assert.Equal(t, "Hello, world!", result["content"])
}

func TestUserMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *UserMsg
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     &UserMsg{Content: "Hello"},
			wantErr: false,
		},
		{
			name:    "empty content",
			msg:     &UserMsg{Content: ""},
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

// --- AssistantMsg Tests ---

func TestAssistantMsg_MarshalJSON_ContentOnly(t *testing.T) {
	msg := &AssistantMsg{Content: "Hello! How can I help?"}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	assert.Equal(t, "Hello! How can I help?", result["content"])
	assert.Nil(t, result["tool_calls"]) // omitempty
}

func TestAssistantMsg_MarshalJSON_ToolCallsOnly(t *testing.T) {
	msg := &AssistantMsg{
		ToolCalls: []ToolCall{
			{
				ID:        "call_123",
				Name:      "get_weather",
				Arguments: map[string]any{"location": "Paris"},
			},
		},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	assert.Empty(t, result["content"]) // omitempty means empty string not present or empty

	toolCalls, ok := result["tool_calls"].([]any)
	require.True(t, ok)
	require.Len(t, toolCalls, 1)

	tc := toolCalls[0].(map[string]any)
	assert.Equal(t, "call_123", tc["id"])
	assert.Equal(t, "get_weather", tc["name"])

	args := tc["arguments"].(map[string]any)
	assert.Equal(t, "Paris", args["location"])
}

func TestAssistantMsg_MarshalJSON_ContentAndToolCalls(t *testing.T) {
	msg := &AssistantMsg{
		Content: "Let me check the weather for you.",
		ToolCalls: []ToolCall{
			{ID: "call_456", Name: "get_weather", Arguments: map[string]any{"location": "London"}},
		},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	assert.Equal(t, "Let me check the weather for you.", result["content"])
	assert.NotNil(t, result["tool_calls"])
}

func TestAssistantMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *AssistantMsg
		wantErr bool
	}{
		{
			name:    "content only",
			msg:     &AssistantMsg{Content: "Hello"},
			wantErr: false,
		},
		{
			name: "tool calls only",
			msg: &AssistantMsg{
				ToolCalls: []ToolCall{{ID: "1", Name: "test"}},
			},
			wantErr: false,
		},
		{
			name: "content and tool calls",
			msg: &AssistantMsg{
				Content:   "Here's the result",
				ToolCalls: []ToolCall{{ID: "1", Name: "test"}},
			},
			wantErr: false,
		},
		{
			name:    "empty - no content or tool calls",
			msg:     &AssistantMsg{},
			wantErr: true,
		},
		{
			name: "invalid tool call - missing ID",
			msg: &AssistantMsg{
				ToolCalls: []ToolCall{{Name: "test"}},
			},
			wantErr: true,
		},
		{
			name: "invalid tool call - missing name",
			msg: &AssistantMsg{
				ToolCalls: []ToolCall{{ID: "1"}},
			},
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

// --- ToolCallResult Tests ---

func TestToolCallResult_MarshalJSON(t *testing.T) {
	msg := &ToolCallResult{
		ToolCallID: "call_123",
		Output:     `{"temperature": 22}`,
		IsError:    false,
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "tool", result["role"])
	assert.Equal(t, "call_123", result["tool_call_id"])
	assert.Equal(t, `{"temperature": 22}`, result["content"]) // Output marshals as content
	// is_error should be omitted when false (omitempty)
}

func TestToolCallResult_MarshalJSON_WithError(t *testing.T) {
	msg := &ToolCallResult{
		ToolCallID: "call_456",
		Output:     "Error: file not found",
		IsError:    true,
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "tool", result["role"])
	assert.Equal(t, "call_456", result["tool_call_id"])
	assert.Equal(t, "Error: file not found", result["content"])
	assert.Equal(t, true, result["is_error"])
}

func TestToolCallResult_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *ToolCallResult
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     &ToolCallResult{ToolCallID: "123", Output: "result"},
			wantErr: false,
		},
		{
			name:    "valid with error flag",
			msg:     &ToolCallResult{ToolCallID: "123", Output: "error", IsError: true},
			wantErr: false,
		},
		{
			name:    "missing tool call id",
			msg:     &ToolCallResult{Output: "result"},
			wantErr: true,
		},
		{
			name:    "missing output",
			msg:     &ToolCallResult{ToolCallID: "123"},
			wantErr: true,
		},
		{
			name:    "empty",
			msg:     &ToolCallResult{},
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

// --- ToolCall Tests ---

func TestToolCall_MarshalJSON(t *testing.T) {
	tc := ToolCall{
		ID:        "call_abc",
		Name:      "search",
		Arguments: map[string]any{"query": "golang", "limit": float64(10)},
	}

	data, err := json.Marshal(tc)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "call_abc", result["id"])
	assert.Equal(t, "search", result["name"])

	args := result["arguments"].(map[string]any)
	assert.Equal(t, "golang", args["query"])
	assert.Equal(t, float64(10), args["limit"])
}

func TestToolCall_UnmarshalJSON(t *testing.T) {
	jsonData := `{"id": "call_xyz", "name": "get_weather", "arguments": {"location": "Paris", "unit": "celsius"}}`

	var tc ToolCall
	err := json.Unmarshal([]byte(jsonData), &tc)
	require.NoError(t, err)

	assert.Equal(t, "call_xyz", tc.ID)
	assert.Equal(t, "get_weather", tc.Name)
	assert.Equal(t, "Paris", tc.Arguments["location"])
	assert.Equal(t, "celsius", tc.Arguments["unit"])
}

func TestToolCall_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := ToolCall{
		ID:        "call_roundtrip",
		Name:      "complex_tool",
		Arguments: map[string]any{"nested": map[string]any{"key": "value"}, "array": []any{1.0, 2.0, 3.0}},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored ToolCall
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.ID, restored.ID)
	assert.Equal(t, original.Name, restored.Name)

	// Check nested structure
	nested := restored.Arguments["nested"].(map[string]any)
	assert.Equal(t, "value", nested["key"])

	arr := restored.Arguments["array"].([]any)
	assert.Len(t, arr, 3)
}

func TestToolCall_Validate(t *testing.T) {
	tests := []struct {
		name    string
		tc      ToolCall
		wantErr bool
	}{
		{
			name:    "valid",
			tc:      ToolCall{ID: "123", Name: "test"},
			wantErr: false,
		},
		{
			name:    "valid with arguments",
			tc:      ToolCall{ID: "123", Name: "test", Arguments: map[string]any{"key": "value"}},
			wantErr: false,
		},
		{
			name:    "missing id",
			tc:      ToolCall{Name: "test"},
			wantErr: true,
		},
		{
			name:    "missing name",
			tc:      ToolCall{ID: "123"},
			wantErr: true,
		},
		{
			name:    "empty",
			tc:      ToolCall{},
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

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	userMsg, ok := msgs[0].(*UserMsg)
	require.True(t, ok, "expected *UserMsg, got %T", msgs[0])
	assert.Equal(t, "Hello", userMsg.Content)
	assert.Equal(t, RoleUser, userMsg.Role())
}

func TestMessages_UnmarshalJSON_AllMessageTypes(t *testing.T) {
	jsonData := `[
		{"role": "system", "content": "You are helpful."},
		{"role": "user", "content": "What's the weather?"},
		{"role": "assistant", "tool_calls": [{"id": "call_1", "name": "get_weather", "arguments": {"location": "Paris"}}]},
		{"role": "tool", "tool_call_id": "call_1", "content": "{\"temp\": 22}", "is_error": false}
	]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 4)

	// Check SystemMsg
	sysMsg, ok := msgs[0].(*SystemMsg)
	require.True(t, ok, "expected *SystemMsg, got %T", msgs[0])
	assert.Equal(t, "You are helpful.", sysMsg.Content)
	assert.Equal(t, RoleSystem, sysMsg.Role())

	// Check UserMsg
	userMsg, ok := msgs[1].(*UserMsg)
	require.True(t, ok, "expected *UserMsg, got %T", msgs[1])
	assert.Equal(t, "What's the weather?", userMsg.Content)
	assert.Equal(t, RoleUser, userMsg.Role())

	// Check AssistantMsg with ToolCalls
	assistantMsg, ok := msgs[2].(*AssistantMsg)
	require.True(t, ok, "expected *AssistantMsg, got %T", msgs[2])
	assert.Equal(t, RoleAssistant, assistantMsg.Role())
	require.Len(t, assistantMsg.ToolCalls, 1)
	assert.Equal(t, "call_1", assistantMsg.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", assistantMsg.ToolCalls[0].Name)
	assert.Equal(t, "Paris", assistantMsg.ToolCalls[0].Arguments["location"])

	// Check ToolCallResult
	toolResult, ok := msgs[3].(*ToolCallResult)
	require.True(t, ok, "expected *ToolCallResult, got %T", msgs[3])
	assert.Equal(t, RoleTool, toolResult.Role())
	assert.Equal(t, "call_1", toolResult.ToolCallID)
	assert.Equal(t, `{"temp": 22}`, toolResult.Output)
	assert.False(t, toolResult.IsError)
}

func TestMessages_UnmarshalJSON_AssistantWithContent(t *testing.T) {
	jsonData := `[{"role": "assistant", "content": "Hello there!"}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	assistantMsg, ok := msgs[0].(*AssistantMsg)
	require.True(t, ok)
	assert.Equal(t, "Hello there!", assistantMsg.Content)
	assert.Empty(t, assistantMsg.ToolCalls)
}

func TestMessages_UnmarshalJSON_AssistantWithContentAndToolCalls(t *testing.T) {
	jsonData := `[{"role": "assistant", "content": "Let me help.", "tool_calls": [{"id": "1", "name": "test", "arguments": {}}]}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	assistantMsg, ok := msgs[0].(*AssistantMsg)
	require.True(t, ok)
	assert.Equal(t, "Let me help.", assistantMsg.Content)
	require.Len(t, assistantMsg.ToolCalls, 1)
	assert.Equal(t, "1", assistantMsg.ToolCalls[0].ID)
}

func TestMessages_UnmarshalJSON_ToolResultWithError(t *testing.T) {
	jsonData := `[{"role": "tool", "tool_call_id": "call_err", "content": "Something went wrong", "is_error": true}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	toolResult, ok := msgs[0].(*ToolCallResult)
	require.True(t, ok)
	assert.Equal(t, "call_err", toolResult.ToolCallID)
	assert.Equal(t, "Something went wrong", toolResult.Output)
	assert.True(t, toolResult.IsError)
}

func TestMessages_UnmarshalJSON_EmptyArray(t *testing.T) {
	jsonData := `[]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	assert.Empty(t, msgs)
}

func TestMessages_UnmarshalJSON_UnknownRole(t *testing.T) {
	jsonData := `[{"role": "unknown", "content": "test"}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown role")
}

func TestMessages_UnmarshalJSON_InvalidJSON(t *testing.T) {
	jsonData := `not valid json`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)

	require.Error(t, err)
}

func TestMessages_UnmarshalJSON_MissingRole(t *testing.T) {
	jsonData := `[{"content": "no role"}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown role")
}

// --- Round-trip Tests ---

func TestMessages_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := Messages{
		&SystemMsg{Content: "You are a helpful assistant."},
		&UserMsg{Content: "What's 2+2?"},
		&AssistantMsg{Content: "The answer is 4."},
		&UserMsg{Content: "Thanks!"},
	}

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal
	var restored Messages
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Verify
	require.Len(t, restored, 4)

	sysMsg := restored[0].(*SystemMsg)
	assert.Equal(t, "You are a helpful assistant.", sysMsg.Content)

	userMsg1 := restored[1].(*UserMsg)
	assert.Equal(t, "What's 2+2?", userMsg1.Content)

	assistantMsg := restored[2].(*AssistantMsg)
	assert.Equal(t, "The answer is 4.", assistantMsg.Content)

	userMsg2 := restored[3].(*UserMsg)
	assert.Equal(t, "Thanks!", userMsg2.Content)
}

func TestMessages_MarshalUnmarshalRoundTrip_WithToolCalls(t *testing.T) {
	original := Messages{
		&UserMsg{Content: "What's the weather in Paris?"},
		&AssistantMsg{
			ToolCalls: []ToolCall{
				{ID: "call_weather", Name: "get_weather", Arguments: map[string]any{"location": "Paris", "unit": "celsius"}},
			},
		},
		&ToolCallResult{ToolCallID: "call_weather", Output: `{"temp": 22, "conditions": "sunny"}`},
		&AssistantMsg{Content: "The weather in Paris is 22°C and sunny."},
	}

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal
	var restored Messages
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Verify
	require.Len(t, restored, 4)

	// Check assistant message with tool calls
	assistantMsg := restored[1].(*AssistantMsg)
	require.Len(t, assistantMsg.ToolCalls, 1)
	assert.Equal(t, "call_weather", assistantMsg.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", assistantMsg.ToolCalls[0].Name)
	assert.Equal(t, "Paris", assistantMsg.ToolCalls[0].Arguments["location"])
	assert.Equal(t, "celsius", assistantMsg.ToolCalls[0].Arguments["unit"])

	// Check tool result
	toolResult := restored[2].(*ToolCallResult)
	assert.Equal(t, "call_weather", toolResult.ToolCallID)
	assert.Equal(t, `{"temp": 22, "conditions": "sunny"}`, toolResult.Output)
	assert.False(t, toolResult.IsError)

	// Check final assistant message
	finalMsg := restored[3].(*AssistantMsg)
	assert.Equal(t, "The weather in Paris is 22°C and sunny.", finalMsg.Content)
}

// --- Message Interface Tests ---

func TestMessage_RoleMethod(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want Role
	}{
		{
			name: "SystemMsg",
			msg:  &SystemMsg{Content: "test"},
			want: RoleSystem,
		},
		{
			name: "UserMsg",
			msg:  &UserMsg{Content: "test"},
			want: RoleUser,
		},
		{
			name: "AssistantMsg",
			msg:  &AssistantMsg{Content: "test"},
			want: RoleAssistant,
		},
		{
			name: "ToolCallResult",
			msg:  &ToolCallResult{ToolCallID: "1", Output: "test"},
			want: RoleTool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.msg.Role())
		})
	}
}

// --- Backwards Compatibility Tests ---

func TestBackwardsCompatibility_OldJSONFormat(t *testing.T) {
	// This tests that the old JSON format (from the old Message struct) can be parsed
	oldFormatJSON := `[
		{"role": "system", "content": "System prompt"},
		{"role": "user", "content": "User message"},
		{"role": "assistant", "content": "Assistant response"},
		{"role": "assistant", "tool_calls": [{"id": "tc1", "name": "tool", "arguments": {"key": "value"}}]},
		{"role": "tool", "content": "Tool result", "tool_call_id": "tc1"}
	]`

	var msgs Messages
	err := json.Unmarshal([]byte(oldFormatJSON), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 5)
	assert.IsType(t, &SystemMsg{}, msgs[0])
	assert.IsType(t, &UserMsg{}, msgs[1])
	assert.IsType(t, &AssistantMsg{}, msgs[2])
	assert.IsType(t, &AssistantMsg{}, msgs[3])
	assert.IsType(t, &ToolCallResult{}, msgs[4])
}

func TestBackwardsCompatibility_MarshalProducesOldFormat(t *testing.T) {
	msgs := Messages{
		&UserMsg{Content: "Hello"},
		&AssistantMsg{ToolCalls: []ToolCall{{ID: "1", Name: "test", Arguments: map[string]any{}}}},
		&ToolCallResult{ToolCallID: "1", Output: "result"},
	}

	data, err := json.Marshal(msgs)
	require.NoError(t, err)

	// Parse as generic JSON to verify structure
	var raw []map[string]any
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)

	// User message
	assert.Equal(t, "user", raw[0]["role"])
	assert.Equal(t, "Hello", raw[0]["content"])

	// Assistant message with tool calls
	assert.Equal(t, "assistant", raw[1]["role"])
	toolCalls := raw[1]["tool_calls"].([]any)
	assert.Len(t, toolCalls, 1)

	// Tool result - Output should be marshaled as "content"
	assert.Equal(t, "tool", raw[2]["role"])
	assert.Equal(t, "1", raw[2]["tool_call_id"])
	assert.Equal(t, "result", raw[2]["content"]) // Output -> content
}
