# Plan: Add Anthropic Thinking & Output Effort

## Problem Statement

We need to align llm.Request with Anthropic's thinking and effort parameters:

1. **`output_config.effort`** — Control response thoroughness (`low/medium/high/max`)
2. **`thinking`** — Sonnet 4.6/Opus 4.6 should use adaptive thinking by default
3. **`cache_control`** — Already exists via `CacheHint`, ensure it's applied to system prompts

Existing `ThinkingEffort` field is kept and used. The provider decides how to map it per model.

## File Map

**Modified:**
- `llm/request.go` — Add `OutputEffort` type and field
- `provider/anthropic/request.go` — Map `ThinkingEffort` + `OutputEffort` to Anthropic API
- `provider/anthropic/claude/provider.go` — Auto-set adaptive thinking for Sonnet 4.6/Opus 4.6

**New:** none
**Deleted:** none

## Types & Mapping

### OutputEffort (NEW - Anthropic only)
```go
type OutputEffort string

const (
    OutputEffortLow    OutputEffort = "low"
    OutputEffortMedium OutputEffort = "medium"
    OutputEffortHigh   OutputEffort = "high"
    OutputEffortMax    OutputEffort = "max"  // Opus 4.6 only
)
```

### Request struct additions
```go
type Request struct {
    // ... existing fields ...
    
    // OutputEffort controls response thoroughness (Anthropic only).
    // Supported on Opus 4.6, Sonnet 4.6, Opus 4.5.
    OutputEffort OutputEffort `json:"output_effort,omitempty"`
}
```

### Anthropic API mapping (in provider)

| Model | ThinkingEffort | Anthropic API |
|-------|---------------|---------------|
| Sonnet 4.6 / Opus 4.6 | any | `thinking: {type: "adaptive"}` |
| Haiku / older models | any | `thinking: {type: "enabled", budget_tokens: N}` |

### OutputEffort mapping
```json
// OutputEffort:
"output_config": {"effort": "medium"}

// If also has OutputFormat JSON:
"output_config": {
  "effort": "medium",
  "format": {"type": "json_schema"}
}
```

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| OutputEffort "max" only works on Opus 4.6 | Error on other models | Only set "max" on Opus 4.6; skip on others |
| Adaptive thinking on non-4.6 models | API error | Only apply adaptive on Sonnet 4.6 / Opus 4.6 |

## Tasks

### Task 1 — Add OutputEffort type to request.go

**Files:** `llm/request.go`

**Steps:**
1. Add `OutputEffort` type with constants: `OutputEffortLow`, `OutputEffortMedium`, `OutputEffortHigh`, `OutputEffortMax`
2. Add `OutputEffort` field to `Request` struct
3. Add validation for `OutputEffort` in `Validate()`
4. Add `Valid()` method for `OutputEffort`

**Verify:** `go build ./...` → exit 0

---

### Task 2 — Map ThinkingEffort and OutputEffort in Anthropic provider

**Files:** `provider/anthropic/request.go`

**Steps:**
1. Update `thinking` struct to support `type: "adaptive"` (add a `Type` field that can be set to "adaptive" or "enabled")
2. In `BuildRequest`:
   - If `ThinkingEffort != ""`:
     - Sonnet 4.6 / Opus 4.6 → `thinking: {type: "adaptive"}`
     - Other models → `thinking: {type: "enabled", budget_tokens: N}` (existing logic)
   - If `OutputEffort != ""` → add `output_config: {effort: "..."}`
3. Ensure `OutputConfig.Format` and `OutputConfig.Effort` can coexist in same object

**Verify:** `go test ./provider/anthropic/...` → all pass

---

### Task 3 — Detect model version in BuildRequest

**Files:** `provider/anthropic/request.go`

**Steps:**
1. Add helper function `isThinkingAdaptiveSupported(model string)` to detect Sonnet 4.6 / Opus 4.6
2. Check model string contains "claude-sonnet-4-6" or "claude-opus-4-6"
3. Use this in the mapping logic from Task 2

**Verify:** Unit test for helper function

---

### Task 4 — Auto-set adaptive thinking for Sonnet 4.6 / Opus 4.6

**Files:** `provider/anthropic/claude/provider.go`

**Steps:**
1. When `ThinkingEffort` is set and model is Sonnet 4.6 / Opus 4.6, use adaptive thinking
2. This matches Claude Code's behavior (they send adaptive by default)
3. For older models, fall back to `enabled` + `budget_tokens`

**Verify:**
```bash
go run ./cmd/llmcli/... infer --log-http -m claude/sonnet "hi" | grep -A2 "thinking"
```
Should show `"type": "adaptive"`

---

### Task 5 — Add --effort CLI flag

**Files:** `cmd/llmcli/cmds/infer.go`

**Steps:**
1. Add `--effort` flag: `low`, `medium`, `high`, `max`
2. Map to `Request.OutputEffort`

**Verify:**
```bash
go run ./cmd/llmcli/... infer --help | grep effort
```

---

### Task 6 — Run full test suite

**Files:** none (verification only)

**Steps:**
1. Run `go test ./... -skip "Integration|TestProviders"`

**Verify:** All tests pass

## Open Questions

None — approach is straightforward.

## Notes

- `ThinkingEffort` is already the unified field — provider maps to appropriate API format
- `OutputEffort` is Anthropic-only — other providers should ignore it
- `ThinkingMode` was considered but not needed — provider decides based on model
