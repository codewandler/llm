# Plan: Interleaved Thinking as Default

**Design**: `.agents/plans/DESIGN-20260413-interleaved-thinking.md`
**Estimated total time**: ~25 minutes

---

### Task 1: Fix `convertMessages` ordering in Anthropic provider

**Files modified**: `provider/anthropic/message.go`
**Estimated time**: 3 minutes

**Code to write** — replace lines 154-169:

```go
		case llm.RoleAssistant:
			var blocks []MessageContentBlock
			for _, p := range m.Parts {
				switch p.Type {
				case msg.PartTypeThinking:
					blocks = append(blocks, Thinking(p.Thinking.Text, p.Thinking.Signature))
				case msg.PartTypeText:
					blocks = append(blocks, Text(p.Text))
				case msg.PartTypeToolCall:
					tc := p.ToolCall
					tub := ToolUse(tc.ID, tc.Name, tc.Args)
					blocks = append(blocks, &tub)
				}
			}
```

**Verification**:
```bash
go build ./provider/anthropic/...
go test ./provider/anthropic/... -run TestBuildRequest_AssistantWithBlocks
```

---

### Task 2: Add interleaved ordering test for Anthropic

**Files modified**: `provider/anthropic/blocks_wire_test.go`
**Estimated time**: 4 minutes

**Code to write** — append new test after existing tests:

```go
// TestBuildRequest_AssistantInterleavedOrder verifies that when an assistant
// message has interleaved parts (thinking between tool calls), the wire
// output preserves the exact emission order.
func TestBuildRequest_AssistantInterleavedOrder(t *testing.T) {
	// Simulate interleaved thinking: think → tool → think → text → tool
	tr := msg.Assistant(
		msg.Thinking("Plan search", "sig-1"),
		msg.ToolCall{ID: "tc1", Name: "search", Args: tool.Args{"q": "go"}},
		msg.Thinking("Evaluate results", "sig-2"),
		msg.Text("Here are the results"),
		msg.ToolCall{ID: "tc2", Name: "fetch", Args: tool.Args{"url": "x"}},
	).Build()

	m := buildRequestMap(t, RequestOptions{
		LLMRequest: llm.Request{
			Model:    "claude-sonnet-4-5",
			Messages: llm.Messages{llm.User("find it"), tr},
		},
	})

	messages := m["messages"].([]any)
	assistantMsg := messages[1].(map[string]any)
	content := assistantMsg["content"].([]any)
	require.Len(t, content, 5, "all 5 interleaved blocks must be present")

	// Verify exact order: thinking, tool_use, thinking, text, tool_use
	assert.Equal(t, "thinking", content[0].(map[string]any)["type"])
	assert.Equal(t, "Plan search", content[0].(map[string]any)["thinking"])
	assert.Equal(t, "sig-1", content[0].(map[string]any)["signature"])

	assert.Equal(t, "tool_use", content[1].(map[string]any)["type"])
	assert.Equal(t, "search", content[1].(map[string]any)["name"])

	assert.Equal(t, "thinking", content[2].(map[string]any)["type"])
	assert.Equal(t, "Evaluate results", content[2].(map[string]any)["thinking"])
	assert.Equal(t, "sig-2", content[2].(map[string]any)["signature"])

	assert.Equal(t, "text", content[3].(map[string]any)["type"])
	assert.Equal(t, "Here are the results", content[3].(map[string]any)["text"])

	assert.Equal(t, "tool_use", content[4].(map[string]any)["type"])
	assert.Equal(t, "fetch", content[4].(map[string]any)["name"])
}
```

**Verification**:
```bash
go test ./provider/anthropic/... -run TestBuildRequest_AssistantInterleavedOrder -v
go test ./provider/anthropic/... -run TestBuildRequest_AssistantWithBlocks -v
```

---

### Task 3: Add `Anthropic-Beta` header to direct Anthropic provider

**Files modified**: `provider/anthropic/anthropic.go`
**Estimated time**: 2 minutes

**Code to write** — add after line 132 (`req.Header.Set("Anthropic-Version", ...)`):

```go
	req.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")
```

**Verification**:
```bash
go build ./provider/anthropic/...
go test ./provider/anthropic/... -run TestNewAPIRequestHeaders -v
```

Note: `TestNewAPIRequestHeaders` must be updated to assert the new header.

---

### Task 4: Update Anthropic header test

**Files modified**: `provider/anthropic/anthropic_test.go`
**Estimated time**: 2 minutes

**Code to write** — add assertion after line 36 (`assert.Equal(t, "application/json", ...)`):

```go
	assert.Equal(t, "interleaved-thinking-2025-05-14", req.Header.Get("Anthropic-Beta"))
```

**Verification**:
```bash
go test ./provider/anthropic/... -run TestNewAPIRequestHeaders -v
```

---

### Task 5: Add `Anthropic-Beta` header to MiniMax provider

**Files modified**: `provider/minimax/minimax.go`
**Estimated time**: 2 minutes

**Code to write** — add after line 153 (`req.Header.Set("Anthropic-Version", ...)`):

```go
	req.Header.Set("Anthropic-Beta", "interleaved-thinking-2025-05-14")
```

**Verification**:
```bash
go build ./provider/minimax/...
go test ./provider/minimax/... -run TestNewAPIRequestHeaders -v
```

Note: `TestNewAPIRequestHeaders` in `minimax_test.go` must be updated to assert the new header.

---

### Task 6: Update MiniMax header test

**Files modified**: `provider/minimax/minimax_test.go`
**Estimated time**: 2 minutes

**Code to write** — add assertion after line 57 (`assert.Equal(t, "application/json", req.Header.Get("Accept"))`):

```go
	assert.Equal(t, "interleaved-thinking-2025-05-14", req.Header.Get("Anthropic-Beta"))
```

**Verification**:
```bash
go test ./provider/minimax/... -run TestNewAPIRequestHeaders -v
```

---

### Task 7: Fix Bedrock assistant message builder — iterate parts in order + include thinking

**Files modified**: `provider/bedrock/bedrock.go`
**Estimated time**: 5 minutes

**Code to write** — replace lines 406-430 (the `case msg.RoleAssistant:` block):

```go
		case msg.RoleAssistant:
			var content []types.ContentBlock
			for _, p := range m.Parts {
				switch p.Type {
				case msg.PartTypeText:
					if p.Text != "" {
						content = append(content, &types.ContentBlockMemberText{Value: p.Text})
					}
				case msg.PartTypeThinking:
					content = append(content, &types.ContentBlockMemberReasoningContent{
						Value: &types.ReasoningContentBlockMemberReasoningText{
							Value: types.ReasoningTextBlock{
								Text:      aws.String(p.Thinking.Text),
								Signature: aws.String(p.Thinking.Signature),
							},
						},
					})
				case msg.PartTypeToolCall:
					tc := p.ToolCall
					inputDoc, err := toDocument(tc.Args)
					if err != nil {
						return nil, fmt.Errorf("marshal tool arguments: %w", err)
					}
					content = append(content, &types.ContentBlockMemberToolUse{
						Value: types.ToolUseBlock{
							ToolUseId: aws.String(tc.ID),
							Name:      aws.String(tc.Name),
							Input:     inputDoc,
						},
					})
				}
			}
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				content = append(content, &types.ContentBlockMemberCachePoint{Value: *cp})
			}
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleAssistant,
				Content: content,
			})
```

**Verification**:
```bash
go build ./provider/bedrock/...
go test ./provider/bedrock/... -v
```

---

### Task 8: Add `anthropic_beta` to Bedrock additional request fields

**Files modified**: `provider/bedrock/bedrock.go`
**Estimated time**: 2 minutes

**Code to write** — add new block after the `reasoning_config` block (after line 579, before the `if len(additionalFields) > 0` check):

```go
	// Enable interleaved thinking beta for Claude models.
	// Harmless no-op for models that don't support it.
	if isClaudeModel(opts.Model) {
		if additionalFields == nil {
			additionalFields = make(map[string]any)
		}
		additionalFields["anthropic_beta"] = []string{"interleaved-thinking-2025-05-14"}
	}
```

**Verification**:
```bash
go build ./provider/bedrock/...
go test ./provider/bedrock/... -v
```

---

### Task 9: Add Bedrock interleaved thinking tests

**Files modified**: `provider/bedrock/bedrock_test.go`
**Estimated time**: 5 minutes

**Code to write** — two new tests:

```go
func TestBuildRequest_AssistantInterleavedThinking(t *testing.T) {
	// Verify assistant messages with interleaved thinking parts
	// serialize in the correct order with reasoning content blocks.
	tr := msg.Assistant(
		msg.Thinking("Plan search", "sig-1"),
		msg.ToolCall{ID: "tc1", Name: "search", Args: tool.Args{"q": "go"}},
		msg.Thinking("Evaluate results", "sig-2"),
		msg.Text("Here are the results"),
	).Build()

	// Build a request with these messages
	input, err := buildTestRequest(t, llm.Request{
		Model:    "us.anthropic.claude-sonnet-4-20250514-v1:0",
		Messages: llm.Messages{llm.User("find it"), tr},
		Thinking: llm.ThinkingOn,
		Effort:   llm.EffortHigh,
	})
	require.NoError(t, err)

	// Check the assistant message content blocks
	require.Len(t, input.Messages, 2) // user + assistant
	assistant := input.Messages[1]
	require.Len(t, assistant.Content, 4)

	// Block 0: reasoning (thinking)
	_, ok := assistant.Content[0].(*types.ContentBlockMemberReasoningContent)
	assert.True(t, ok, "block 0 must be reasoning content")

	// Block 1: tool_use
	_, ok = assistant.Content[1].(*types.ContentBlockMemberToolUse)
	assert.True(t, ok, "block 1 must be tool use")

	// Block 2: reasoning (thinking)
	_, ok = assistant.Content[2].(*types.ContentBlockMemberReasoningContent)
	assert.True(t, ok, "block 2 must be reasoning content")

	// Block 3: text
	_, ok = assistant.Content[3].(*types.ContentBlockMemberText)
	assert.True(t, ok, "block 3 must be text")
}

func TestBuildRequest_AnthropicBetaHeader(t *testing.T) {
	// Verify the anthropic_beta field is set in additional request fields
	// for Claude models.
	input, err := buildTestRequest(t, llm.Request{
		Model:    "us.anthropic.claude-sonnet-4-20250514-v1:0",
		Messages: llm.Messages{llm.User("hello")},
	})
	require.NoError(t, err)

	require.NotNil(t, input.AdditionalModelRequestFields)

	var fields map[string]any
	err = input.AdditionalModelRequestFields.UnmarshalSmithyDocument(&fields)
	require.NoError(t, err)

	beta, ok := fields["anthropic_beta"]
	require.True(t, ok, "anthropic_beta must be present")

	betaList, ok := beta.([]any)
	require.True(t, ok, "anthropic_beta must be an array")
	require.Contains(t, betaList, "interleaved-thinking-2025-05-14")
}
```

Note: A `buildTestRequest` helper may need to be created (or use existing test infrastructure). Check `bedrock_test.go` for the existing pattern.

**Verification**:
```bash
go test ./provider/bedrock/... -run TestBuildRequest_AssistantInterleavedThinking -v
go test ./provider/bedrock/... -run TestBuildRequest_AnthropicBetaHeader -v
```

---

### Task 10: Fix OpenRouter assistant message builder ordering

**Files modified**: `provider/openrouter/openrouter.go`
**Estimated time**: 3 minutes

**Code to write** — replace lines 320-351 (the `case msg.RoleAssistant:` block):

```go
		case msg.RoleAssistant:
			mp := messagePayload{
				Role:    "assistant",
				Content: m.Text(),
			}
			var reasoningText strings.Builder
			for _, p := range m.Parts {
				switch p.Type {
				case msg.PartTypeThinking:
					if p.Thinking == nil {
						continue
					}
					reasoningText.WriteString(p.Thinking.Text)
					mp.ReasoningDetails = append(mp.ReasoningDetails, reasoningDetailInput{
						Type:      "reasoning.text",
						Text:      p.Thinking.Text,
						Signature: p.Thinking.Signature,
					})
				case msg.PartTypeToolCall:
					argsJSON, _ := json.Marshal(p.ToolCall.Args)
					mp.ToolCalls = append(mp.ToolCalls, toolCallItem{
						ID:   p.ToolCall.ID,
						Type: "function",
						Function: functionCall{
							Name:      p.ToolCall.Name,
							Arguments: string(argsJSON),
						},
					})
				}
			}
			if reasoningText.Len() > 0 {
				mp.Reasoning = reasoningText.String()
			}
			r.Messages = append(r.Messages, mp)
```

**Verification**:
```bash
go build ./provider/openrouter/...
go test ./provider/openrouter/... -run TestBuildRequest_AssistantThinkingIncluded -v
```

---

### Task 11: Add OpenRouter interleaved ordering test

**Files modified**: `provider/openrouter/openrouter_test.go`
**Estimated time**: 3 minutes

**Code to write** — append new test:

```go
func TestBuildRequest_AssistantInterleavedThinkingOrder(t *testing.T) {
	// Verify that reasoning_details preserve the part emission order
	// when thinking is interleaved with tool calls.
	body, err := buildRequest(llm.Request{
		Model: "test/model",
		Messages: msg.BuildTranscript(
			msg.User("find it"),
			msg.Assistant(
				msg.Thinking("Plan search", "sig-1"),
				msg.ToolCall{ID: "tc1", Name: "search", Args: tool.Args{"q": "go"}},
				msg.Thinking("Evaluate results", "sig-2"),
				msg.Text("Here are the results"),
			),
		),
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	messages := req["messages"].([]any)
	assistantMsg := messages[1].(map[string]any)

	// reasoning_details must have 2 entries in the correct order
	details := assistantMsg["reasoning_details"].([]any)
	require.Len(t, details, 2)
	assert.Equal(t, "Plan search", details[0].(map[string]any)["text"])
	assert.Equal(t, "sig-1", details[0].(map[string]any)["signature"])
	assert.Equal(t, "Evaluate results", details[1].(map[string]any)["text"])
	assert.Equal(t, "sig-2", details[1].(map[string]any)["signature"])

	// reasoning string must concatenate thinking texts
	assert.Equal(t, "Plan searchEvaluate results", assistantMsg["reasoning"])

	// tool_calls must be present
	toolCalls := assistantMsg["tool_calls"].([]any)
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "tc1", toolCalls[0].(map[string]any)["id"])
}
```

**Verification**:
```bash
go test ./provider/openrouter/... -run TestBuildRequest_AssistantInterleavedThinkingOrder -v
```

---

### Task 12: Full build and test sweep

**Files modified**: none
**Estimated time**: 2 minutes

**Verification**:
```bash
go build ./...
go vet ./...
go test ./...
```

All must pass. If any existing test broke due to the ordering change, fix it in this task.

---

## Task Dependency Graph

```
Task 1  (fix anthropic ordering)
  └─ Task 2  (test interleaved ordering)
Task 3  (anthropic beta header)
  └─ Task 4  (update header test)
Task 5  (minimax beta header)
  └─ Task 6  (update header test)
Task 7  (fix bedrock ordering + thinking)
Task 8  (bedrock beta field)
  └─ Task 9  (bedrock tests)
Task 10 (fix openrouter ordering)
  └─ Task 11 (openrouter test)
Task 12 (full sweep) ── depends on all above
```

Tasks 1, 3, 5, 7, 8, 10 are independent of each other (different files).
Tests (2, 4, 6, 9, 11) depend on their implementation task.
Task 12 depends on everything.
