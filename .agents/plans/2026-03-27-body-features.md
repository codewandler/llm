# Plan: Add Anthropic-Specific Body Features

## Problem Statement

The llmcli requests are missing several Anthropic-specific body parameters that Claude Code sends:

1. **`thinking`** — Sonnet/Opus should use `adaptive` thinking, Haiku should use `enabled` with `budget_tokens`
2. **`output_config.effort`** — Control response thoroughness (`low`, `medium`, `high`, `max`)
3. **`cache_control`** — Prompt caching for expensive system prompts
4. **`context_management`** — Session memory management

## File Map

**Modified:**
- `request.go` — Add `Thinking` and `Effort` fields to `Request` struct
- `provider/anthropic/anthropic.go` — Build `thinking`, `output_config`, `cache_control`, `context_management` in request body
- `provider/anthropic/claude/provider.go` — Add `thinking` and `output_config.effort` based on model
- `llm/message.go` — Add `CacheControl` field to messages for prompt caching

**New:** none
**Deleted:** none

## API Reference Summary

### thinking
```json
// Sonnet 4.6 / Opus 4.6 (recommended)
"thinking": {"type": "adaptive"}

// Haiku / older models
"thinking": {"type": "enabled", "budget_tokens": 16000}
```

### output_config.effort
```json
"output_config": {
  "effort": "medium"  // "low" | "medium" | "high" | "max" (Opus only)
}
```

### cache_control
```json
// On system prompt or user message
{"type": "text", "content": "...", "cache_control": {"type": "ephemeral"}}

// On message content block
{"type": "text", "content": "...", "cache_control": {"type": "ephemeral"}}
```

### context_management
```json
"context_management": {
  "edits": [{"type": "clear_thinking_20251015", "keep": "all"}]
}
```

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| Thinking enabled increases output tokens significantly | Users may hit max_tokens unexpectedly | Ensure max_tokens is scaled appropriately when thinking is enabled |
| cache_control on large prompts may fail if not within cache limits | API error on invalid cache control | Handle API errors gracefully |
| Effort "max" only works on Opus 4.6 | Error on other models | Only set "max" on Opus 4.6 |
| Changing request body format may break existing tests | Test failures | Update tests to match new request format |

## Tasks

### Task 1 — Add Thinking and Effort fields to Request struct

**Files:** `request.go`

**Steps:**
1. Add `Thinking *ThinkingConfig` field to `Request` struct
2. Add `Effort EffortLevel` field to `Request` struct  
3. Add `ThinkingConfig` struct with `Type` and `BudgetTokens` fields
4. Add `EffortLevel` type with constants: `EffortLow`, `EffortMedium`, `EffortHigh`, `EffortMax`
5. Add validation for these new fields

**Verify:** `go build ./...` → exit 0

---

### Task 2 — Add CacheControl field to Message Content

**Files:** `llm/message.go`

**Steps:**
1. Add `CacheControl *CacheControl` field to `Content` interface
2. Add `CacheControl` struct with `Type` field
3. Update `TextContent`, `ImageContent` etc. to include the field
4. Update `Validate()` for messages

**Verify:** `go build ./...` → exit 0

---

### Task 3 — Build thinking block in Anthropic provider

**Files:** `provider/anthropic/anthropic.go`

**Steps:**
1. In `buildRequest`, add logic to build `thinking` block from `Request.Thinking`
2. Add `output_config` with `effort` from `Request.Effort`
3. Handle different model requirements (Sonnet 4.6 → adaptive, Haiku → enabled)

**Verify:** `go test ./provider/anthropic/...` → all pass

---

### Task 4 — Set thinking defaults based on model in claude provider

**Files:** `provider/anthropic/claude/provider.go`

**Steps:**
1. In `buildRequest`, auto-set thinking based on model:
   - Sonnet 4.6 / Opus 4.6 → `{"type": "adaptive"}`
   - Haiku → `{"type": "enabled", "budget_tokens": 31999}` (or 16000)
2. Only override if not explicitly set by user

**Verify:** `go build ./... && go run ./cmd/llmcli/... infer --log-http -m claude/sonnet "hi" | grep thinking`

---

### Task 5 — Add cache_control to first system prompt

**Files:** `provider/anthropic/anthropic.go`

**Steps:**
1. Add `cache_control: {"type": "ephemeral"}` to first system message content
2. This enables prompt caching for expensive system prompts

**Verify:** Check logged request body contains cache_control

---

### Task 6 — Add context_management to claude provider

**Files:** `provider/anthropic/claude/provider.go`

**Steps:**
1. Add `context_management.edits` with `clear_thinking_20251015` to request body
2. This handles session memory/continuity

**Verify:** Check logged request body contains context_management

---

### Task 7 — Run full test suite

**Files:** none (verification only)

**Steps:**
1. Run `go test ./... -skip "Integration|TestProviders"`

**Verify:** All tests pass

## Open Questions

1. **Default effort level**: Should we default to "medium" (balanced) or match Claude Code's behavior (no effort = high)?
   - **Recommendation**: Don't set effort by default (let API use its default = high), only set when user explicitly requests it.

2. **Thinking for Sonnet 4.5/Opus 4.5**: Should we use adaptive or enabled+budget_tokens?
   - Adaptive is only on 4.6 models. For 4.5, should use enabled+budget_tokens.
   - **Recommendation**: Only enable adaptive on Sonnet 4.6 and Opus 4.6.

3. **cache_control type**: `ephemeral` vs `persistent`?
   - Anthropic docs recommend `ephemeral` for most cases.
   - **Recommendation**: Use `ephemeral` for prompt caching.
