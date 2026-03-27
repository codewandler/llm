package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/tool"
)

// --- System Tests ---

func TestSystemMsg_MarshalJSON(t *testing.T) {
	msg := &systemMsg{textMsg: textMsg{role: RoleSystem, content: "You are a helpful assistant."}}

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
		msg     *systemMsg
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     &systemMsg{textMsg: textMsg{role: RoleSystem, content: "You are helpful."}},
			wantErr: false,
		},
		{
			name:    "empty content",
			msg:     &systemMsg{textMsg: textMsg{role: RoleSystem, content: ""}},
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
	msg := &userMsg{textMsg: textMsg{role: RoleUser, content: "Hello, world!"}}

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
		msg     *userMsg
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     &userMsg{textMsg: textMsg{role: RoleUser, content: "Hello"}},
			wantErr: false,
		},
		{
			name:    "empty content",
			msg:     &userMsg{textMsg: textMsg{role: RoleUser, content: ""}},
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

// --- AssistantMessage Tests ---

func TestAssistantMsg_MarshalJSON_ContentOnly(t *testing.T) {
	msg := Assistant("Hello! How can I help?")

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	assert.Nil(t, result["content"])      // no longer emitted
	assert.NotNil(t, result["content_blocks"]) // blocks are used instead
	assert.Nil(t, result["tool_calls"]) // omitempty
}

func TestAssistantMsg_MarshalJSON_ToolCallsOnly(t *testing.T) {
	msg := &assistantMsg{
		textMsg:   textMsg{role: RoleAssistant},
		toolCalls: []tool.Call{tool.NewToolCall("call_123", "get_weather", map[string]any{"location": "Paris"})},
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

	args := tc["args"].(map[string]any)
	assert.Equal(t, "Paris", args["location"])
}

func TestAssistantMsg_MarshalJSON_ContentAndToolCalls(t *testing.T) {
	msg := &assistantMsg{
		textMsg:       textMsg{role: RoleAssistant},
		contentBlocks: []ContentBlock{{Kind: ContentBlockKindText, Text: "Let me check the weather for you."}},
		toolCalls:     []tool.Call{tool.NewToolCall("call_456", "get_weather", map[string]any{"location": "London"})},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "assistant", result["role"])
	assert.Nil(t, result["content"])          // no longer emitted
	assert.NotNil(t, result["content_blocks"]) // blocks are used instead
	assert.NotNil(t, result["tool_calls"])
}

func TestAssistantMsg_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *assistantMsg
		wantErr bool
	}{
		{
			name:    "content only",
			msg:     &assistantMsg{textMsg: textMsg{role: RoleAssistant}, contentBlocks: []ContentBlock{{Kind: ContentBlockKindText, Text: "Hello"}}},
			wantErr: false,
		},
		{
			name: "tool calls only",
			msg: &assistantMsg{
				toolCalls: []tool.Call{tool.NewToolCall("1", "test", nil)},
			},
			wantErr: false,
		},
		{
			name: "content and tool calls",
			msg: &assistantMsg{
				textMsg:       textMsg{role: RoleAssistant},
				contentBlocks: []ContentBlock{{Kind: ContentBlockKindText, Text: "Here's the result"}},
				toolCalls:     []tool.Call{tool.NewToolCall("1", "test", nil)},
			},
			wantErr: false,
		},
		{
			name:    "empty - no content or tool calls",
			msg:     &assistantMsg{},
			wantErr: true,
		},
		{
			name: "invalid tool call - missing ToolCallID",
			msg: &assistantMsg{
				toolCalls: []tool.Call{tool.NewToolCall("", "test", nil)},
			},
			wantErr: true,
		},
		{
			name: "invalid tool call - missing name",
			msg: &assistantMsg{
				toolCalls: []tool.Call{tool.NewToolCall("1", "", nil)},
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

// --- ToolResult Tests ---

func TestToolCallResult_MarshalJSON(t *testing.T) {
	msg := &toolMsg{
		textMsg:    textMsg{role: RoleTool, content: `{"temperature": 22}`},
		toolCallID: "call_123",
		isError:    false,
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "tool", result["role"])
	assert.Equal(t, "call_123", result["tool_call_id"])
	assert.Equal(t, `{"temperature": 22}`, result["content"]) // ToolOutput marshals as content
	// is_error should be omitted when false (omitempty)
}

func TestToolCallResult_MarshalJSON_WithError(t *testing.T) {
	msg := &toolMsg{
		textMsg:    textMsg{role: RoleTool, content: "Error: file not found"},
		toolCallID: "call_456",
		isError:    true,
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
		msg     *toolMsg
		wantErr bool
	}{
		{
			name:    "valid",
			msg:     &toolMsg{textMsg: textMsg{content: "result"}, toolCallID: "123"},
			wantErr: false,
		},
		{
			name:    "valid with error flag",
			msg:     &toolMsg{textMsg: textMsg{content: "error"}, toolCallID: "123", isError: true},
			wantErr: false,
		},
		{
			name:    "missing tool call id",
			msg:     &toolMsg{textMsg: textMsg{content: "result"}},
			wantErr: true,
		},
		{
			name:    "missing output",
			msg:     &toolMsg{toolCallID: "123"},
			wantErr: true,
		},
		{
			name:    "empty",
			msg:     &toolMsg{},
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

	// tool.Call interface doesn't support direct unmarshal, so we just verify marshal succeeds
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

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	userMsg, ok := msgs[0].(*userMsg)
	require.True(t, ok, "expected *userMsg, got %T", msgs[0])
	assert.Equal(t, "Hello", userMsg.content)
	assert.Equal(t, RoleUser, userMsg.Role())
}

func TestMessages_UnmarshalJSON_AllMessageTypes(t *testing.T) {
	jsonData := `[
		{"role": "system", "content": "You are helpful."},
		{"role": "user", "content": "What's the weather?"},
		{"role": "assistant", "tool_calls": [{"id": "call_1", "name": "get_weather", "args": {"location": "Paris"}}]},
		{"role": "tool", "tool_call_id": "call_1", "content": "{\"temp\": 22}", "is_error": false}
	]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 4)

	// Check System
	sysMsg, ok := msgs[0].(*systemMsg)
	require.True(t, ok, "expected *systemMsg, got %T", msgs[0])
	assert.Equal(t, "You are helpful.", sysMsg.content)
	assert.Equal(t, RoleSystem, sysMsg.Role())

	// Check UserMsg
	userMsg, ok := msgs[1].(*userMsg)
	require.True(t, ok, "expected *userMsg, got %T", msgs[1])
	assert.Equal(t, "What's the weather?", userMsg.content)
	assert.Equal(t, RoleUser, userMsg.Role())

	// Check AssistantMessage with ToolCalls
	asstMsg, ok := msgs[2].(*assistantMsg)
	require.True(t, ok, "expected *assistantMsg, got %T", msgs[2])
	assert.Equal(t, RoleAssistant, asstMsg.Role())
	require.Len(t, asstMsg.toolCalls, 1)
	assert.Equal(t, "call_1", asstMsg.toolCalls[0].ToolCallID())
	assert.Equal(t, "get_weather", asstMsg.toolCalls[0].ToolName())
	assert.Equal(t, "Paris", asstMsg.toolCalls[0].ToolArgs()["location"])

	// Check ToolResult
	toolResult, ok := msgs[3].(*toolMsg)
	require.True(t, ok, "expected *toolMsg, got %T", msgs[3])
	assert.Equal(t, RoleTool, toolResult.Role())
	assert.Equal(t, "call_1", toolResult.toolCallID)
	assert.Equal(t, `{"temp": 22}`, toolResult.content)
	assert.False(t, toolResult.isError)
}

func TestMessages_UnmarshalJSON_AssistantWithContent(t *testing.T) {
	jsonData := `[{"role": "assistant", "content": "Hello there!"}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	asstMsg, ok := msgs[0].(*assistantMsg)
	require.True(t, ok)
	// Old JSON with flat "content" is restored via Assistant() → text block is auto-wrapped.
	assert.Equal(t, "Hello there!", AssistantText(asstMsg))
	assert.Empty(t, asstMsg.toolCalls)
}

func TestMessages_UnmarshalJSON_AssistantWithContentAndToolCalls(t *testing.T) {
	jsonData := `[{"role": "assistant", "content": "Let me help.", "tool_calls": [{"id": "1", "name": "test", "arguments": {}}]}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	asstMsg, ok := msgs[0].(*assistantMsg)
	require.True(t, ok)
	// Old JSON with flat "content" is restored via Assistant() → text block is auto-wrapped.
	assert.Equal(t, "Let me help.", AssistantText(asstMsg))
	require.Len(t, asstMsg.toolCalls, 1)
	assert.Equal(t, "1", asstMsg.toolCalls[0].ToolCallID())
}

func TestMessages_UnmarshalJSON_ToolResultWithError(t *testing.T) {
	jsonData := `[{"role": "tool", "tool_call_id": "call_err", "content": "Something went wrong", "is_error": true}]`

	var msgs Messages
	err := json.Unmarshal([]byte(jsonData), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 1)

	toolResult, ok := msgs[0].(*toolMsg)
	require.True(t, ok)
	assert.Equal(t, "call_err", toolResult.toolCallID)
	assert.Equal(t, "Something went wrong", toolResult.content)
	assert.True(t, toolResult.isError)
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
		System("You are a helpful assistant."),
		User("What's 2+2?"),
		Assistant("The answer is 4."),
		User("Thanks!"),
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

	sysMsg := restored[0].(*systemMsg)
	assert.Equal(t, "You are a helpful assistant.", sysMsg.content)

	userMsg1 := restored[1].(*userMsg)
	assert.Equal(t, "What's 2+2?", userMsg1.content)

	asstMsg := restored[2].(*assistantMsg)
	assert.Equal(t, "The answer is 4.", AssistantText(asstMsg))

	userMsg2 := restored[3].(*userMsg)
	assert.Equal(t, "Thanks!", userMsg2.content)
}

func TestMessages_MarshalUnmarshalRoundTrip_WithToolCalls(t *testing.T) {
	original := Messages{
		User("What's the weather in Paris?"),
		Assistant("", tool.NewToolCall("call_weather", "get_weather", map[string]any{"location": "Paris", "unit": "celsius"})),
		Tool("call_weather", `{"temp": 22, "conditions": "sunny"}`),
		Assistant("The weather in Paris is 22°C and sunny."),
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
	asstMsg := restored[1].(*assistantMsg)
	require.Len(t, asstMsg.toolCalls, 1)
	assert.Equal(t, "call_weather", asstMsg.toolCalls[0].ToolCallID())
	assert.Equal(t, "get_weather", asstMsg.toolCalls[0].ToolName())
	assert.Equal(t, "Paris", asstMsg.toolCalls[0].ToolArgs()["location"])
	assert.Equal(t, "celsius", asstMsg.toolCalls[0].ToolArgs()["unit"])

	// Check tool result
	toolResult := restored[2].(*toolMsg)
	assert.Equal(t, "call_weather", toolResult.toolCallID)
	assert.Equal(t, `{"temp": 22, "conditions": "sunny"}`, toolResult.content)
	assert.False(t, toolResult.isError)

	// Check final assistant message
	finalMsg := restored[3].(*assistantMsg)
	assert.Equal(t, "The weather in Paris is 22°C and sunny.", AssistantText(finalMsg))
}

// --- Message Interface Tests ---

func TestMessage_RoleMethod(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want Role
	}{
		{
			name: "System",
			msg:  System("test"),
			want: RoleSystem,
		},
		{
			name: "UserMsg",
			msg:  User("test"),
			want: RoleUser,
		},
		{
			name: "AssistantMessage",
			msg:  Assistant("test"),
			want: RoleAssistant,
		},
		{
			name: "ToolResult",
			msg:  Tool("1", "test"),
			want: RoleTool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.msg.Role())
		})
	}
}

// --- Content Block JSON Round-Trip Tests ---

// TestAssistantMsg_MarshalJSON_WithContentBlocks verifies that an assistant message
// created via Assistant() has its ContentBlocks serialized as "content_blocks" in JSON.
func TestAssistantMsg_MarshalJSON_WithContentBlocks(t *testing.T) {
	msg := Assistant("The answer is 42.")

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	assert.Equal(t, "assistant", raw["role"])
	// content field is no longer emitted; only content_blocks.
	assert.Nil(t, raw["content"])
	blocks, ok := raw["content_blocks"].([]any)
	require.True(t, ok, "content_blocks must be present in JSON")
	require.Len(t, blocks, 1)
	b := blocks[0].(map[string]any)
	assert.Equal(t, "text", b["kind"])
	assert.Equal(t, "The answer is 42.", b["text"])
}

// TestAssistantMsg_MarshalJSON_WithThinkingBlock verifies that thinking blocks
// (including their signatures) survive marshal → unmarshal with zero mutation.
func TestAssistantMsg_MarshalJSON_WithThinkingBlock(t *testing.T) {
	const sig = "eyJhbGciOiJFZERTQSJ9.opaque-signature"
	msg := AssistantWithBlocks([]ContentBlock{
		{Kind: ContentBlockKindThinking, Text: "My reasoning", Signature: sig},
		{Kind: ContentBlockKindText, Text: "The answer"},
	})

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	blocks, ok := raw["content_blocks"].([]any)
	require.True(t, ok)
	require.Len(t, blocks, 2)

	b0 := blocks[0].(map[string]any)
	assert.Equal(t, "thinking", b0["kind"])
	assert.Equal(t, "My reasoning", b0["text"])
	assert.Equal(t, sig, b0["signature"], "signature must survive JSON marshal")

	b1 := blocks[1].(map[string]any)
	assert.Equal(t, "text", b1["kind"])
	assert.Equal(t, "The answer", b1["text"])
}

// TestMessages_RoundTrip_ContentBlocks verifies that a Messages slice containing
// an assistant message with thinking+text blocks survives a full JSON round-trip,
// with ContentBlocks, signatures, and Content() all preserved.
func TestMessages_RoundTrip_ContentBlocks(t *testing.T) {
	const sig = "sig-round-trip-must-survive"

	original := Messages{
		User("think about this"),
		AssistantWithBlocks([]ContentBlock{
			{Kind: ContentBlockKindThinking, Text: "reasoning", Signature: sig},
			{Kind: ContentBlockKindText, Text: "answer"},
		}),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored Messages
	require.NoError(t, json.Unmarshal(data, &restored))
	require.Len(t, restored, 2)

	am, ok := restored[1].(AssistantMessage)
	require.True(t, ok)

	blocks := am.ContentBlocks()
	require.Len(t, blocks, 2)
	assert.Equal(t, ContentBlockKindThinking, blocks[0].Kind)
	assert.Equal(t, "reasoning", blocks[0].Text)
	assert.Equal(t, sig, blocks[0].Signature, "signature must survive round-trip")
	assert.Equal(t, ContentBlockKindText, blocks[1].Kind)
	assert.Equal(t, "answer", blocks[1].Text)

	// AssistantText() returns the text for providers that use flat text.
	assert.Equal(t, "answer", AssistantText(am))
}

// TestMessages_RoundTrip_OldFormat_NoBlocks verifies backward compatibility:
// old JSON without "content_blocks" deserializes correctly via the flat-text path,
// and the restored message auto-wraps into a ContentBlock.
func TestMessages_RoundTrip_OldFormat_NoBlocks(t *testing.T) {
	oldJSON := `[{"role":"assistant","content":"Legacy response"}]`

	var msgs Messages
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &msgs))
	require.Len(t, msgs, 1)

	am, ok := msgs[0].(AssistantMessage)
	require.True(t, ok)

	// Old format: no blocks in JSON → Assistant() is called → auto-wraps into text block.
	assert.Equal(t, "Legacy response", AssistantText(am))
	blocks := am.ContentBlocks()
	require.Len(t, blocks, 1, "Assistant() must auto-wrap text into a ContentBlock")
	assert.Equal(t, ContentBlockKindText, blocks[0].Kind)
	assert.Equal(t, "Legacy response", blocks[0].Text)
}

func TestBackwardsCompatibility_OldJSONFormat(t *testing.T) {
	oldFormatJSON := `[
		{"role": "system", "content": "System prompt"},
		{"role": "user", "content": "User message"},
		{"role": "assistant", "content": "Assistant response"},
		{"role": "assistant", "tool_calls": [{"id": "tc1", "name": "tool", "args": {"key": "value"}}]},
		{"role": "tool", "content": "Tool result", "tool_call_id": "tc1"}
	]`

	var msgs Messages
	err := json.Unmarshal([]byte(oldFormatJSON), &msgs)
	require.NoError(t, err)

	require.Len(t, msgs, 5)
	assert.IsType(t, &systemMsg{}, msgs[0])
	assert.IsType(t, &userMsg{}, msgs[1])
	assert.IsType(t, &assistantMsg{}, msgs[2])
	assert.IsType(t, &assistantMsg{}, msgs[3])
	assert.IsType(t, &toolMsg{}, msgs[4])
}

func TestBackwardsCompatibility_MarshalProducesOldFormat(t *testing.T) {
	msgs := Messages{
		User("Hello"),
		Assistant("", tool.NewToolCall("1", "test", map[string]any{})),
		Tool("1", "result"),
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

	// Tool result - ToolOutput should be marshaled as "content"
	assert.Equal(t, "tool", raw[2]["role"])
	assert.Equal(t, "1", raw[2]["tool_call_id"])
	assert.Equal(t, "result", raw[2]["content"]) // ToolOutput -> content
}
