# Plan: Match Claude Code Request Headers 100%

## Problem Statement

The llmcli provider sends HTTP requests to Anthropic that differ from what Claude Code sends:
1. **Accept-Encoding**: Only `gzip` instead of `gzip, deflate, br, zstd`
2. **Connection**: Missing `keep-alive` header
3. **User-Agent**: Version `2.1.72` instead of `2.1.85`

This causes unnecessary overhead (no best-compression), potential connection churn, and version skew that could affect API behavior.

## File Map

**Modified:**
- `provider/anthropic/claude/provider.go` — Add missing headers (Accept-Encoding, Connection) and update User-Agent version
- `http.go` — Add support for brotli (`br`) and zstd decompression so the HTTP client can handle all compression types sent in Accept-Encoding
- `provider/anthropic/anthropic.go` — Add Accept-Encoding header for consistency with claude provider

**New:** none
**Deleted:** none

## Risk Register

| Risk | Impact | Mitigation |
|---|---|---|
| Adding `Accept-Encoding: gzip, deflate, br, zstd` without client-side decompression support breaks responses | High — responses may be unreadable | Implement br/zstd decompression in http.go transport |
| Go's default HTTP transport doesn't support brotli or zstd | High — need custom transport or don't claim support | Write a decompressing transport wrapper |
| `Connection: keep-alive` is the default in HTTP/1.1 but explicit may help proxies | Low — mostly informational | Still add for exact header match |
| Changing User-Agent may affect API behavior/rate limits | Medium — version number could matter | Update to match exactly |

## Tasks

### Task 1 — Update User-Agent version in claude provider

**Files:** `provider/anthropic/claude/provider.go`

**Steps:**
1. Change `claudeUserAgent = "claude-cli/2.1.72 (external, sdk-cli)"` to `claudeUserAgent = "claude-cli/2.1.85 (external, sdk-cli)"`

**Verify:** `go build ./...` → exit 0

---

### Task 2 — Add missing headers (Accept-Encoding, Connection) to claude provider

**Files:** `provider/anthropic/claude/provider.go`

**Steps:**
1. Add `Accept-Encoding: gzip, deflate, br, zstd` header in `newAPIRequest`
2. Add `Connection: keep-alive` header in `newAPIRequest`

**Verify:** `go build ./... && go test ./provider/anthropic/claude/...` → all pass

---

### Task 3 — Implement brotli and zstd decompression transport

**Files:** `http.go`

**Steps:**
1. Add imports for `compress/flate`, `compress/gzip`, `compress/zstd`, and `github.com/andybalholm/brotli`
2. Create a `decompressingTransport` that wraps another `http.RoundTripper`
3. On request: add `Accept-Encoding` header if not present (or ensure it includes br, zstd)
4. On response: detect Content-Encoding and wrap body with appropriate decompressor (gzip.Reader, flate.Reader, brotli.Reader, zstd.NewReader)
5. Wire this transport into `NewHttpClient` after the logging transport

**Verify:** `go build ./...` → exit 0

---

### Task 4 — Add Accept-Encoding header to base anthropic provider for consistency

**Files:** `provider/anthropic/anthropic.go`

**Steps:**
1. Add `Accept-Encoding: gzip, deflate, br, zstd` header in `newAPIRequest`

**Verify:** `go build ./... && go test ./provider/anthropic/...` → all pass

---

### Task 5 — Verify headers match Claude Code exactly

**Files:** none (verification only)

**Steps:**
1. Run `llmcli infer --log-http "1+1"` with logging enabled
2. Compare request headers against Claude Code captured headers
3. Ensure all 19 headers match exactly

**Verify:** Manual comparison of logged headers

---

### Task 6 — Run full test suite

**Files:** none (verification only)

**Steps:**
1. Run `go test ./...`

**Verify:** `go test ./...` → all tests pass

## Open Questions

1. **brotli/zstd dependencies**: Do we want to add `github.com/andybalholm/brotli` and `github.com/klauspost/compress/zstd` as dependencies, or should we only claim support for what Go stdlib handles (`gzip, deflate`)?
   - If we claim `br, zstd` in Accept-Encoding but can't decompress, responses will be garbled.
   - **Recommendation**: Implement full support for all four to match Claude Code exactly.

2. **Version update**: Should the version be a constant that can be updated via build flags, or hardcoded?
   - **Recommendation**: Keep as constant for now; version bumps can be part of release process.
