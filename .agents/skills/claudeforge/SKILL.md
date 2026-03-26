# ClaudeForge Skill

## Objective

ClaudeForge is an HTTP proxy tool that captures and logs all traffic between Claude Code CLI and the Anthropic API. It is used to analyze how Claude Code sends requests and receives responses, enabling us to track changes in the official CLI's behavior over time.

## Use Cases

1. **Diff Analysis**: When Claude Code releases an update, run a request through both the proxy and directly to identify what changed (headers, body fields, new features).
2. **Debugging**: Capture request/response pairs to debug authentication, caching, or streaming issues.
3. **Feature Tracking**: Monitor for new fields like `context_management`, `output_config`, `thinking.type`, etc.

## Quick Start

```bash
# Terminal 1 - Build and start proxy
cd .agents/skills/claudeforge/scripts
go build -o /tmp/claudeforge claudeforge.go
nohup /tmp/claudeforge > /tmp/claudeforge.log 2>&1 &
sleep 2

# Terminal 2 - Run Claude through proxy
ANTHROPIC_BASE_URL=http://localhost:7890 claude -p --tools "" --system-prompt "" "your prompt"
```

## Command Flags

- `-port`: Port to listen on (default: `7890`)

## Output

Logs are written to `.agents/logs/claudeforge/` (relative to the repository root):

| File | Description |
|------|-------------|
| `request_<timestamp>.json` | Individual request (headers + body) |
| `response_<timestamp>.json` | Individual response (headers + SSE body) |
| `requests.log` | Combined request log (appended) |
| `responses.log` | Combined response log (appended) |

## Key Fields to Track

### Headers
- `User-Agent` - Claude CLI version (e.g., `claude-cli/2.1.83`)
- `X-Stainless-Timeout` - Request timeout (e.g., `300`)
- `Anthropic-Beta` - Beta feature flags
- `Authorization` - Bearer token (mask in logs)

### Body Fields
- `thinking.type` - Thinking mode (`"adaptive"`, `"enabled"`, or absent)
- `context_management` - Context editing instructions
- `output_config.effort` - Reasoning effort setting
- `stream` - Streaming flag
- `cache_control.ttl` - Cache TTL (`"1h"` or absent)
- `metadata.user_id` - User identification structure
- `tools` - Tool definitions (LSP, etc.)

### Billing Header (`x-anthropic-billing-header`)
- `cc_version` - Claude Code version
- `cc_entrypoint` - Entry point (`sdk-cli`)
- `cch` - Dynamic hash (newer versions)

## Analysis Workflow

1. Start proxy
2. Run minimal Claude request through proxy
3. Run same request directly (without proxy)
4. Compare logs - look for:
   - New or missing headers
   - Changed header values
   - New body fields
   - Changed body field values
   - New beta flags

## Minimal Test Command

```bash
ANTHROPIC_BASE_URL=http://localhost:7890 claude -p --tools "" --system-prompt "" "answer just the result: 1+1"
```

Flags explained:
- `-p` - Print mode (non-interactive)
- `--tools ""` - Disable tools (attempt to minimize request)
- `--system-prompt ""` - Empty system prompt
- `--strict-mcp-config "{}"` - Strict MCP config (optional, if needed)

## Report Template

When documenting findings, structure the report as:

```markdown
# Claude CLI vs Provider Diff Report

## Date
[ISO date of capture]

## Version Comparison
| Component | CLI | Provider |
|-----------|-----|----------|
| User-Agent | X.X.X | X.X.X |
| cc_version | X.X.X | X.X.X |

## Header Differences
| Header | CLI | Provider | Diff |
|--------|-----|----------|------|

## Body Field Differences
| Field | CLI | Provider | Notes |
|-------|-----|----------|-------|

## New Fields in CLI
- [field]: [description]

## Action Items
- [ ] [Update/changelog item]
```

## Troubleshooting

### Proxy won't start
- Check if port is already in use: `ss -tlnp | grep 7890`
- Kill existing process: `pkill -f claudeforge`

### Request not reaching proxy
- Verify `ANTHROPIC_BASE_URL` is set correctly
- Check proxy is listening: `curl localhost:7890`

### Response shows "Decompression error"
- Ensure gzip decompression is working (built into proxy transport)

## Maintenance

The proxy code lives in `.agents/skills/claudeforge/scripts/claudeforge.go`.

When Claude Code updates:
1. Run a capture with the updated CLI
2. Compare against previous captures or provider code
3. Document differences in `.agents/skills/claudeforge/assets/`
4. Update provider code as needed
