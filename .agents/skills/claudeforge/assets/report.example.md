# Claude CLI vs Provider Diff Report

## Date
2026-03-26

## Version Comparison

| Component | CLI (Captured) | Provider (`provider/anthropic/claude/`) |
|-----------|-----------------|----------------------------------------|
| `User-Agent` | `claude-cli/2.1.83 (external, sdk-cli)` | `claude-cli/2.1.72 (external, sdk-cli)` |
| `cc_version` in billing | `2.1.83.812` | `2.1.72.364` |
| `X-Stainless-Timeout` | `300` | `600` |
| `Anthropic-Beta` | `claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24` | Same (beta string unchanged) |

## Header Differences

| Header | CLI | Provider | Diff |
|--------|-----|----------|------|
| `User-Agent` | `claude-cli/2.1.83` | `claude-cli/2.1.72` | **Version mismatch** |
| `X-Stainless-Timeout` | `300` | `600` | **CLI uses shorter timeout** |
| Billing `cc_version` | `2.1.83.812` | `2.1.72.364` | **Version mismatch** |
| Billing `cch` | Present (dynamic hash) | **Not present** | **New field** |

## Body Field Differences

| Field | CLI | Provider | Notes |
|-------|-----|----------|-------|
| `thinking` | `{type: "adaptive"}` | **Not sent** (only when `ReasoningEffort != ""`) | **CLI always sends adaptive thinking** |
| `context_management` | `{edits: [{type: "clear_thinking_20251015", keep: "all"}]}` | **Not sent** | **New field in CLI** |
| `output_config` | `{effort: "medium"}` | **Not sent** | **New field in CLI** |
| `stream` | `true` | **Not sent** | **Explicit in CLI** |
| `cache_control.ttl` on system/messages | `"1h"` | **Not sent** (unless user provides `CacheHint`) | **CLI caches with 1h TTL by default** |
| `metadata.user_id` | Nested JSON: `{"device_id":"...","account_uuid":"...","session_id":"..."}` | Simple: `user_<id>_account_<uuid>_session_<id>` | **Different structure** |

## New Fields in CLI (Not in Provider)

### `thinking`
```json
"thinking": {
  "type": "adaptive"
}
```
- `type: "adaptive"` is sent regardless of `ReasoningEffort`
- Our provider only sends `thinking` when `ReasoningEffort != ""` with `type: "enabled"`

### `context_management`
```json
"context_management": {
  "edits": [
    {
      "type": "clear_thinking_20251015",
      "keep": "all"
    }
  ]
}
```
- Controls thinking/history clearing behavior
- Not supported in our provider at all

### `output_config`
```json
"output_config": {
  "effort": "medium"
}
```
- Controls reasoning effort at output level
- Maps to `ReasoningEffort` but with different semantics
- Our provider doesn't send this

### `stream: true`
- CLI explicitly sets `stream: true`
- Our provider relies on API default

### `cache_control` with `ttl: "1h"`
- System prompts get `cache_control: {type: "ephemeral", ttl: "1h"}`
- User messages get `cache_control: {type: "ephemeral", ttl: "1h"}`
- Our provider only sets this if user provides `CacheHint`

## Billing Header Details

**CLI sends:**
```
x-anthropic-billing-header: cc_version=2.1.83.812; cc_entrypoint=sdk-cli; cch=32b25;
```

**Our provider sends:**
```
x-anthropic-billing-header: cc_version=2.1.72.364; cc_entrypoint=sdk-cli;
```

The `cch` field is a dynamic per-request hash that we don't generate.

## SSE Response Differences

### CLI Response (Streaming)
```
event: message_start
event: content_block_start
event: ping
event: content_block_delta
event: content_block_stop
event: message_delta
event: message_stop
```

### Provider Response
- Provider uses non-streaming JSON response by default

## Action Items

- [ ] Update `claudeUserAgent` from `2.1.72` to `2.1.83`
- [ ] Update `cc_version` from `2.1.72.364` to `2.1.83.812`
- [ ] Update `X-Stainless-Timeout` from `600` to `300`
- [ ] Investigate `cch` hash generation (optional, may be optional field)
- [ ] Always send `thinking.type: "adaptive"` instead of only when `ReasoningEffort` set
- [ ] Add `context_management` support
- [ ] Add `output_config` support
- [ ] Default `cache_control.ttl` to `"1h"` for system/messages
- [ ] Restructure `metadata.user_id` to match nested format

## Minimal Request Capture (CLI)

```json
{
  "model": "claude-sonnet-4-6",
  "messages": [{
    "role": "user",
    "content": [
      {"type": "text", "text": "...", "cache_control": {"type": "ephemeral", "ttl": "1h"}},
      {"type": "text", "text": "answer just the result: 1+1"}
    ]
  }],
  "system": [
    {"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.83.812; cc_entrypoint=sdk-cli; cch=...", "cache_control": {"type": "ephemeral", "ttl": "1h"}},
    {"type": "text", "text": "You are a Claude agent...", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
  ],
  "tools": [{"name": "LSP", ...}],
  "metadata": {"user_id": "{\"device_id\":\"...\",\"account_uuid\":\"...\",\"session_id\":\"...\"}"},
  "max_tokens": 32000,
  "thinking": {"type": "adaptive"},
  "context_management": {"edits": [{"type": "clear_thinking_20251015", "keep": "all"}]},
  "output_config": {"effort": "medium"},
  "stream": true
}
```
