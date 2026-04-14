# Plan: Silently downgrade forced tool_choice when thinking is on

**Estimated time**: 5 minutes

---

### Task 1: Replace error with silent downgrade for ToolChoiceRequired

**Files modified**: `provider/anthropic/request.go`
**Estimated time**: 3 minutes

**Current behavior** (lines 200-211):
- `ToolChoiceRequired` + thinking → returns error
- `ToolChoiceTool` + thinking → silently downgrades to `ToolChoiceAuto`

**Desired behavior**:
- Both `ToolChoiceRequired` and `ToolChoiceTool` + thinking → silently downgrade to `ToolChoiceAuto`

**Code to write** — replace lines 200-211 with a single block:

```go
	// Anthropic API restriction: thinking cannot be combined with forced
	// tool_choice. Silently downgrade to auto so the request succeeds.
	if req.Thinking != nil && req.Thinking.Type != "disabled" {
		switch llmRequest.ToolChoice.(type) {
		case llm.ToolChoiceRequired, llm.ToolChoiceTool:
			llmRequest.ToolChoice = llm.ToolChoiceAuto{}
		}
	}
```

This removes the `fmt` import (no longer needed for the error).

**Verification**:
```bash
go build ./provider/anthropic/...
go test ./provider/anthropic/... -v
```

---

### Task 2: Add test covering the downgrade

**Files modified**: `provider/anthropic/request_extra_test.go` (or existing test file)
**Estimated time**: 2 minutes

**Code to write**:

```go
func TestBuildRequest_ThinkingDowngradesToolChoice(t *testing.T) {
	for _, tc := range []struct {
		name       string
		toolChoice llm.ToolChoice
	}{
		{"required", llm.ToolChoiceRequired{}},
		{"specific_tool", llm.ToolChoiceTool{Name: "search"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildRequestMap(t, RequestOptions{
				LLMRequest: llm.Request{
					Model:      "claude-sonnet-4-5",
					Messages:   llm.Messages{llm.User("hello")},
					Thinking:   llm.ThinkingOn,
					ToolChoice: tc.toolChoice,
					Tools:      []tool.Definition{tool.NewSpec[struct{}]("search", "search").Definition()},
				},
			})
			// Must not error; tool_choice must be downgraded to auto
			require.Equal(t, map[string]any{"type": "auto"}, m["tool_choice"])
		})
	}
}
```

**Verification**:
```bash
go test ./provider/anthropic/... -run TestBuildRequest_ThinkingDowngradesToolChoice -v
```
