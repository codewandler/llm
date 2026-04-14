# PLAN: cmd/miniagent — Agentic CLI

**Date**: 2026-05-14  
**Design**: [DESIGN-miniagent.md](DESIGN-miniagent.md) (v4)  
**Review**: [REVIEW-plan-miniagent.md](REVIEW-plan-miniagent.md) — all 6 issues applied  
**Refinement**: v2 — additional fixes from self-review  
**Estimated time**: ~90 minutes (12 tasks)

---

## Dependency Graph

```
1 (scaffold)
├── 2 (system prompt + test)
├── 3 (truncation + test)
│   └── 4 (bash handler + test)
├── 5 (format helpers + test)
│   └── 6 (step display + usage printing + test)
│
└──── 7 (agent struct) ← depends on 2, 4, 6
      └── 8 (RunTurn loop) ← depends on 7
          ├── 9 (RunTurn tests) ← depends on 8
          ├── 10 (REPL + test) ← depends on 8
          └── 11 (main.go) ← depends on 8, 10
              └── 12 (smoke test) ← depends on 11
```

Tasks 2, 3, 5 have no interdependencies — they can be implemented in any order after Task 1.

---

## Task 1: Scaffold directory structure

**Files created**:  
- `cmd/miniagent/main.go`  
- `cmd/miniagent/agent/agent.go`  
- `cmd/miniagent/agent/display.go`  
- `cmd/miniagent/agent/tools.go`  
- `cmd/miniagent/agent/system.go`  
- `cmd/miniagent/agent/repl.go`  

**Estimated time**: 2 minutes

Create all files with package declarations only:

```go
// cmd/miniagent/main.go
package main

func main() {}
```

```go
// cmd/miniagent/agent/*.go  (each file)
package agent
```

**Verification**:
```bash
go build ./cmd/miniagent/...
```

---

## Task 2: System prompt builder + test

**Files modified**: `cmd/miniagent/agent/system.go`  
**Files created**: `cmd/miniagent/agent/system_test.go`  
**Depends on**: 1  
**Estimated time**: 4 minutes

### Test first

```go
// cmd/miniagent/agent/system_test.go
package agent

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestBuildSystemPrompt(t *testing.T) {
    tests := []struct {
        name     string
        workspace string
        custom   string
        wantAll  []string
        wantNone []string
    }{
        {
            name:      "default includes workspace and bash mention",
            workspace: "/home/user/project",
            custom:    "",
            wantAll:   []string{"/home/user/project", "bash", "## Workspace"},
        },
        {
            name:      "custom replaces body but keeps workspace",
            workspace: "/tmp/work",
            custom:    "You are a pirate assistant.",
            wantAll:   []string{"You are a pirate assistant.", "/tmp/work", "## Workspace"},
            wantNone:  []string{"helpful terminal assistant"},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := BuildSystemPrompt(tt.workspace, tt.custom)
            for _, s := range tt.wantAll {
                assert.Contains(t, got, s)
            }
            for _, s := range tt.wantNone {
                assert.NotContains(t, got, s)
            }
        })
    }
}
```

### Implementation

```go
// cmd/miniagent/agent/system.go
package agent

import "fmt"

const defaultSystemBody = `You are a helpful terminal assistant. You complete tasks by running bash commands.
Think step by step. When the task is done, respond with a clear summary of what you accomplished.
Do not ask for confirmation — just proceed with the task.`

// BuildSystemPrompt returns the full system prompt. If customBody is non-empty
// it replaces the default body; the workspace section is always appended.
func BuildSystemPrompt(workspace, customBody string) string {
    body := defaultSystemBody
    if customBody != "" {
        body = customBody
    }
    return fmt.Sprintf(
        "%s\n\n## Workspace\nYou are working in: %s\nAll relative paths resolve from this directory.\n",
        body, workspace,
    )
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run TestBuildSystemPrompt -v
```

---

## Task 3: Output truncation helper + test

**Files modified**: `cmd/miniagent/agent/tools.go`  
**Files created**: `cmd/miniagent/agent/tools_test.go`  
**Depends on**: 1  
**Estimated time**: 4 minutes

### Test first

```go
// cmd/miniagent/agent/tools_test.go
package agent

import (
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestTruncateBytes(t *testing.T) {
    tests := []struct {
        name      string
        input     string
        max       int
        truncated bool
    }{
        {"under limit", "hello", 100, false},
        {"at limit", "hello", 5, false},
        {"over limit", strings.Repeat("x", 200), 100, true},
        {"empty", "", 100, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := truncateBytes([]byte(tt.input), tt.max)
            if tt.truncated {
                assert.Contains(t, result, "[...truncated")
                assert.Contains(t, result, "200 bytes")
            } else {
                assert.Equal(t, tt.input, result)
                assert.NotContains(t, result, "truncated")
            }
        })
    }
}

func TestTruncateBytes_LargeOutput(t *testing.T) {
    // Simulate a command producing output larger than maxOutputBytes (20 KB)
    big := strings.Repeat("x", 30*1024) // 30 KB
    result := truncateBytes([]byte(big), maxOutputBytes)
    assert.Contains(t, result, "[...truncated")
    assert.Contains(t, result, "30720 bytes")
    // The visible content is maxOutputBytes + the marker line
    assert.Less(t, len(result), 25*1024, "truncated result should be much smaller than input")
}
```

### Implementation

```go
// cmd/miniagent/agent/tools.go
package agent

import "fmt"

const maxOutputBytes = 20 * 1024 // 20 KB

func truncateBytes(b []byte, max int) string {
    if len(b) <= max {
        return string(b)
    }
    return string(b[:max]) + fmt.Sprintf(
        "\n[...truncated, showing first %d of %d bytes]", max, len(b),
    )
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run TestTruncateBytes -v
```

---

## Task 4: Bash tool spec + handler + test

**Files modified**: `cmd/miniagent/agent/tools.go`, `cmd/miniagent/agent/tools_test.go`  
**Depends on**: 3  
**Estimated time**: 8 minutes

### Test first

Append to `tools_test.go`:

```go
// Merge with Task 3 imports — the complete import block for tools_test.go:
import (
    "context"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/codewandler/llm/tool"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNewBashHandler(t *testing.T) {
    workspace := t.TempDir()
    timeout := 5 * time.Second

    tests := []struct {
        name        string
        command     string
        timeout     time.Duration
        wantContain string
    }{
        {"echo", "echo hello", timeout, "hello"},
        {"non-zero exit", "exit 42", timeout, "exit 42"},
        {"timeout", "sleep 10", 100 * time.Millisecond, "timeout"},
        {"workspace is cwd", "pwd", timeout, workspace},
        {"stderr captured", "echo err >&2", timeout, "err"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            handler := NewBashHandler(workspace, tt.timeout)
            call := tool.NewToolCall("test-id", "bash", map[string]any{"command": tt.command})
            out, err := handler.Handle(context.Background(), call)
            require.NoError(t, err, "handler must never return a Go error")
            assert.Contains(t, fmt.Sprint(out), tt.wantContain)
        })
    }
}

func TestBashDefinition(t *testing.T) {
    def := BashDefinition()
    assert.Equal(t, "bash", def.Name)
    assert.NotEmpty(t, def.Description)
}
```

### Implementation

Add to `cmd/miniagent/agent/tools.go`:

```go
import (
    "context"
    "os/exec"
    "time"

    "github.com/codewandler/llm/tool"
)

// BashParams is the typed input for the bash tool.
type BashParams struct {
    Command string `json:"command" jsonschema:"description=Shell command to execute,required"`
}

// BashResult is the typed output returned to the model as JSON.
type BashResult struct {
    Output string `json:"output"`
}

// BashDefinition returns the tool.Definition for the bash tool.
func BashDefinition() tool.Definition {
    return tool.NewSpec[BashParams](
        "bash",
        "Execute a bash command in the workspace directory. Returns combined stdout and stderr.",
    ).Definition()
}

// NewBashHandler creates a NamedHandler that executes bash commands
// in the given workspace with a per-command timeout.
func NewBashHandler(workspace string, timeout time.Duration) tool.NamedHandler {
    return tool.NewHandler[BashParams, BashResult]("bash",
        func(ctx context.Context, in BashParams) (*BashResult, error) {
            cmdCtx, cancel := context.WithTimeout(ctx, timeout)
            defer cancel()

            cmd := exec.CommandContext(cmdCtx, "bash", "-c", in.Command)
            cmd.Dir = workspace
            out, err := cmd.CombinedOutput()
            output := truncateBytes(out, maxOutputBytes)

            if cmdCtx.Err() == context.DeadlineExceeded {
                return &BashResult{
                    Output: fmt.Sprintf("timeout: command exceeded %s", timeout),
                }, nil
            }
            if err != nil {
                if exitErr, ok := err.(*exec.ExitError); ok {
                    return &BashResult{
                        Output: fmt.Sprintf("exit %d:\n%s", exitErr.ExitCode(), output),
                    }, nil
                }
                return &BashResult{Output: fmt.Sprintf("error: %s", err)}, nil
            }
            return &BashResult{Output: output}, nil
        },
    )
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run "TestNewBashHandler|TestBashDefinition" -v
```

---

## Task 5: Display — formatting helpers + test

**Files modified**: `cmd/miniagent/agent/display.go`  
**Files created**: `cmd/miniagent/agent/display_test.go`  
**Depends on**: 1  
**Estimated time**: 8 minutes

### Test first

```go
// cmd/miniagent/agent/display_test.go
package agent

import (
    "strings"
    "testing"

    "github.com/codewandler/llm/usage"
    "github.com/stretchr/testify/assert"
)

func TestFormatTokenCount(t *testing.T) {
    tests := []struct {
        input int
        want  string
    }{
        {0, "0"},
        {42, "42"},
        {999, "999"},
        {1000, "1\u2009000"},
        {8432, "8\u2009432"},
        {12345, "12\u2009345"},
        {100000, "100\u2009000"},
        {1234567, "1\u2009234\u2009567"},
    }
    for _, tt := range tests {
        t.Run(tt.want, func(t *testing.T) {
            assert.Equal(t, tt.want, formatTokenCount(tt.input))
        })
    }
}

func TestFormatCost(t *testing.T) {
    tests := []struct {
        name string
        cost float64
        want string
    }{
        {"zero", 0, ""},
        {"tiny", 0.00001, "$0.000010"},
        {"small", 0.0023, "$0.0023"},
        {"medium", 0.0412, "$0.0412"},
        {"dollar", 1.24, "$1.24"},
        {"large", 12.50, "$12.50"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, formatCost(tt.cost))
        })
    }
}

func TestTruncateDisplay(t *testing.T) {
    assert.Equal(t, "hello", truncateDisplay("hello", 300))

    long := strings.Repeat("x", 400)
    result := truncateDisplay(long, 300)
    assert.Equal(t, 303, len(result))
    assert.True(t, strings.HasSuffix(result, "..."))
}

func TestFormatUsageParts(t *testing.T) {
    t.Run("all fields", func(t *testing.T) {
        rec := usage.Record{
            Tokens: usage.TokenItems{
                {Kind: usage.KindInput, Count: 1204},
                {Kind: usage.KindCacheRead, Count: 8432},
                {Kind: usage.KindOutput, Count: 87},
            },
            Cost: usage.Cost{Total: 0.0023},
        }
        parts := formatUsageParts(rec)
        assert.Contains(t, parts, "input: 1\u2009204")
        assert.Contains(t, parts, "cache_r: 8\u2009432")
        assert.Contains(t, parts, "output: 87")
        assert.Contains(t, parts, "cost: $0.0023")
    })

    t.Run("zero tokens omitted", func(t *testing.T) {
        rec := usage.Record{
            Tokens: usage.TokenItems{
                {Kind: usage.KindInput, Count: 100},
                {Kind: usage.KindOutput, Count: 50},
            },
        }
        parts := formatUsageParts(rec)
        assert.Contains(t, parts, "input: 100")
        assert.Contains(t, parts, "output: 50")
        assert.NotContains(t, parts, "cache")
        assert.NotContains(t, parts, "cost")
    })

    t.Run("empty record", func(t *testing.T) {
        assert.Equal(t, "", formatUsageParts(usage.Record{}))
    })
}

func TestExtractBashOutput(t *testing.T) {
    tests := []struct {
        name  string
        input any
        want  string
    }{
        {"json result", `{"output":"hello"}`, "hello"},
        {"plain string", "just text", "just text"},
        {"non-string", 42, "42"},
        {"empty json", `{"output":""}`, ""},
        {"malformed json", `{bad`, `{bad`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.want, extractBashOutput(tt.input))
        })
    }
}
```

### Implementation

```go
// cmd/miniagent/agent/display.go
package agent

import (
    "encoding/json"
    "fmt"
    "io"
    "strings"

    "github.com/codewandler/llm/usage"
)

// ANSI escape codes
const (
    ansiReset        = "\033[0m"
    ansiBold         = "\033[1m"
    ansiDim          = "\033[2m"
    ansiBrightRed    = "\033[91m"
    ansiBrightGreen  = "\033[92m"
    ansiBrightYellow = "\033[93m"
    ansiBrightCyan   = "\033[96m"
)

const thinSpace = '\u2009'

// formatTokenCount formats an integer with thin-space thousands separators.
func formatTokenCount(n int) string {
    s := fmt.Sprintf("%d", n)
    if len(s) <= 3 {
        return s
    }
    var b strings.Builder
    remainder := len(s) % 3
    for i, c := range s {
        if i > 0 && i%3 == remainder {
            b.WriteRune(thinSpace)
        }
        b.WriteRune(c)
    }
    return b.String()
}

// formatCost formats a dollar cost with adaptive precision.
// Returns "" for zero cost.
func formatCost(cost float64) string {
    if cost == 0 {
        return ""
    }
    switch {
    case cost < 0.0001:
        return fmt.Sprintf("$%.6f", cost)
    case cost < 1.0:
        return fmt.Sprintf("$%.4f", cost)
    default:
        return fmt.Sprintf("$%.2f", cost)
    }
}

// truncateDisplay truncates a string for terminal display.
func truncateDisplay(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "..."
}

// formatUsageParts builds "input: N  cache_r: N  output: N  cost: $X".
// Shared by step, turn, and session usage display.
func formatUsageParts(rec usage.Record) string {
    kindLabels := []struct {
        kind  usage.TokenKind
        label string
    }{
        {usage.KindInput, "input"},
        {usage.KindCacheRead, "cache_r"},
        {usage.KindCacheWrite, "cache_w"},
        {usage.KindOutput, "output"},
        {usage.KindReasoning, "reason"},
    }
    var parts []string
    for _, kl := range kindLabels {
        count := rec.Tokens.Count(kl.kind)
        if count == 0 {
            continue
        }
        parts = append(parts, fmt.Sprintf("%s: %s", kl.label, formatTokenCount(count)))
    }
    if cs := formatCost(rec.Cost.Total); cs != "" {
        parts = append(parts, fmt.Sprintf("cost: %s", cs))
    }
    return strings.Join(parts, "  ")
}

// extractBashOutput extracts the human-readable output from a tool result.
// The handler returns JSON like {"output":"hello"} — this parses it to "hello".
// Falls back to fmt.Sprint for anything unexpected.
func extractBashOutput(raw any) string {
    s, ok := raw.(string)
    if !ok {
        return fmt.Sprint(raw)
    }
    var result BashResult
    if err := json.Unmarshal([]byte(s), &result); err != nil {
        return s // not JSON — return as-is
    }
    return result.Output
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run "TestFormat|TestTruncateDisplay|TestExtractBash" -v
```

---

## Task 6: Display — step display state machine, headers, usage/result printing + test

**Files modified**: `cmd/miniagent/agent/display.go`, `cmd/miniagent/agent/display_test.go`  
**Depends on**: 5  
**Estimated time**: 10 minutes

### Test first

Append to `display_test.go`:

```go
import "bytes"

func TestStepDisplay_StateTransitions(t *testing.T) {
    t.Run("reasoning then text", func(t *testing.T) {
        var buf bytes.Buffer
        sd := newStepDisplay(&buf)

        sd.WriteReasoning("thinking...")
        sd.WriteText("answer")
        sd.End()

        out := buf.String()
        assert.Contains(t, out, "thinking...")
        assert.Contains(t, out, "answer")
        assert.Contains(t, out, ansiDim)
        assert.Contains(t, out, ansiReset)
    })

    t.Run("text only", func(t *testing.T) {
        var buf bytes.Buffer
        sd := newStepDisplay(&buf)

        sd.WriteText("hello ")
        sd.WriteText("world")
        sd.End()

        out := buf.String()
        assert.Contains(t, out, "hello world")
        assert.NotContains(t, out, ansiDim)
    })

    t.Run("tool call resets state", func(t *testing.T) {
        var buf bytes.Buffer
        sd := newStepDisplay(&buf)

        sd.WriteText("let me check")
        sd.PrintToolCall("bash", "ls -la")
        sd.End()

        out := buf.String()
        assert.Contains(t, out, "let me check")
        assert.Contains(t, out, "🔧 bash")
        assert.Contains(t, out, "$ ls -la")
    })
}
```

### Implementation

Append to `cmd/miniagent/agent/display.go`:

```go
// ---------------------------------------------------------------------------
// Step display state machine
// ---------------------------------------------------------------------------

type displayState int

const (
    stateIdle displayState = iota
    stateReasoning
    stateText
)

type stepDisplay struct {
    w     io.Writer
    state displayState
}

func newStepDisplay(w io.Writer) *stepDisplay {
    return &stepDisplay{w: w, state: stateIdle}
}

// WriteReasoning outputs a reasoning token chunk in dim.
func (d *stepDisplay) WriteReasoning(chunk string) {
    if d.state == stateIdle {
        fmt.Fprint(d.w, ansiDim)
        d.state = stateReasoning
    }
    fmt.Fprint(d.w, chunk)
}

// WriteText outputs a text token chunk in normal weight.
func (d *stepDisplay) WriteText(chunk string) {
    switch d.state {
    case stateIdle:
        fmt.Fprint(d.w, "\n")
    case stateReasoning:
        fmt.Fprintf(d.w, "%s\n\n", ansiReset)
    }
    if d.state != stateText {
        d.state = stateText
    }
    fmt.Fprint(d.w, chunk)
}

// PrintToolCall displays a tool call header. Resets any open ANSI state.
func (d *stepDisplay) PrintToolCall(name, command string) {
    switch d.state {
    case stateReasoning:
        fmt.Fprintf(d.w, "%s\n", ansiReset)
    case stateText:
        fmt.Fprintln(d.w)
    }
    d.state = stateIdle
    fmt.Fprintf(d.w, "\n%s🔧 %s%s\n", ansiBrightYellow, name, ansiReset)
    fmt.Fprintf(d.w, "   %s$ %s%s\n", ansiDim, command, ansiReset)
}

// End closes any open ANSI state. Call after Result() returns.
func (d *stepDisplay) End() {
    switch d.state {
    case stateReasoning:
        fmt.Fprintf(d.w, "%s\n", ansiReset)
    case stateText:
        fmt.Fprintln(d.w)
    }
    d.state = stateIdle
}

// ---------------------------------------------------------------------------
// Step header
// ---------------------------------------------------------------------------

func printStepHeader(w io.Writer, step, maxSteps int) {
    fmt.Fprintf(w, "\n%s── %s💭 Step %d/%d%s %s────────────────────────────────%s\n",
        ansiDim, ansiBold+ansiBrightCyan, step, maxSteps, ansiReset, ansiDim, ansiReset,
    )
}

// ---------------------------------------------------------------------------
// Tool result display  [REVIEW FIX #2: no command reference — calls shown live]
// ---------------------------------------------------------------------------

// printToolResult displays a single tool result line.
// Commands are NOT repeated here — they were already shown live via ToolCallEvent.
func printToolResult(w io.Writer, output string, isError bool) {
    prefix := ansiBrightGreen + "✓" + ansiReset
    if isError {
        prefix = ansiBrightRed + "✗" + ansiReset
    }
    display := truncateDisplay(strings.TrimSpace(output), 300)
    if display == "" {
        display = "(no output)"
    }
    fmt.Fprintf(w, "%s %s\n", prefix, display)
}

// ---------------------------------------------------------------------------
// Usage lines
// ---------------------------------------------------------------------------

func printStepUsage(w io.Writer, step int, rec usage.Record) {
    parts := formatUsageParts(rec)
    if parts == "" {
        return
    }
    fmt.Fprintf(w, "%s   ── step %d ── %s%s\n", ansiDim, step, parts, ansiReset)
}

func printTurnUsage(w io.Writer, turnID string, rec usage.Record) {
    parts := formatUsageParts(rec)
    if parts == "" {
        return
    }
    fmt.Fprintf(w, "%s   ── turn %s ── %s%s\n", ansiDim, turnID, parts, ansiReset)
}

// PrintSessionUsage prints the session-total usage line.
// Exported — called from main.go for one-shot mode.
func PrintSessionUsage(w io.Writer, rec usage.Record) {
    parts := formatUsageParts(rec)
    if parts == "" {
        return
    }
    fmt.Fprintf(w, "── session ── %s\n", parts)
}

// ---------------------------------------------------------------------------
// Error display
// ---------------------------------------------------------------------------

func printError(w io.Writer, err error) {
    fmt.Fprintf(w, "\n%sError: %s%s\n", ansiBrightRed, err, ansiReset)
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run "TestStepDisplay" -v
go build ./cmd/miniagent/...
go vet ./cmd/miniagent/...
```

---

## Task 7: Agent struct, constructor, options

**Files modified**: `cmd/miniagent/agent/agent.go`  
**Depends on**: 2, 4, 6  
**Estimated time**: 5 minutes

This task writes the complete `agent.go` file. Task 8 appends `RunTurn` and helpers.
The import block includes ALL imports needed for both tasks — Task 8 does not
add new imports.

### Implementation

```go
// cmd/miniagent/agent/agent.go
package agent

import (
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "time"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/msg"
    "github.com/codewandler/llm/tool"
    "github.com/codewandler/llm/usage"
)

// Agent runs an agentic loop: LLM → bash tool → LLM → bash tool → ...
// A single Agent instance is reused across REPL turns; conversation history
// and usage records accumulate across turns.
type Agent struct {
    provider    llm.Provider
    messages    msg.Messages
    tracker     *usage.Tracker
    toolDefs    []tool.Definition
    toolHandler tool.NamedHandler
    model       string
    maxSteps    int
    maxTokens   int
    out         io.Writer
}

// Option configures the Agent.
type Option func(*Agent)

// WithModel sets the model alias or full path (default: "default").
func WithModel(m string) Option { return func(a *Agent) { a.model = m } }

// WithMaxSteps sets the maximum agent loop iterations per turn (default: 30).
func WithMaxSteps(n int) Option { return func(a *Agent) { a.maxSteps = n } }

// WithMaxTokens sets the maximum output tokens per LLM call (default: 16000).
func WithMaxTokens(n int) Option { return func(a *Agent) { a.maxTokens = n } }

// WithOutput sets the output writer (default: os.Stdout).
// Tests pass a *bytes.Buffer to capture and suppress output.
func WithOutput(w io.Writer) Option { return func(a *Agent) { a.out = w } }

// New creates an Agent. workspace must be an absolute path to an existing
// directory. cmdTimeout limits each individual bash command.
func New(
    provider llm.Provider,
    workspace string,
    cmdTimeout time.Duration,
    systemOverride string,
    opts ...Option,
) *Agent {
    a := &Agent{
        provider:  provider,
        model:     "default",
        maxSteps:  30,
        maxTokens: 16_000,
        out:       os.Stdout,
    }
    for _, o := range opts {
        o(a)
    }

    a.tracker = usage.NewTracker(
        usage.WithCostCalculator(usage.Default()),
    )

    // System prompt with cache hint for REPL efficiency
    prompt := BuildSystemPrompt(workspace, systemOverride)
    a.messages = msg.Messages{
        msg.System(prompt).Cache(msg.CacheTTL1h).Build(),
    }

    a.toolDefs = []tool.Definition{BashDefinition()}
    a.toolHandler = NewBashHandler(workspace, cmdTimeout)

    return a
}

// Tracker returns the usage tracker for session-level reporting.
func (a *Agent) Tracker() *usage.Tracker { return a.tracker }

// Out returns the output writer (for REPL to write to the same destination).
func (a *Agent) Out() io.Writer { return a.out }
```

**Verification**:
```bash
go build ./cmd/miniagent/...
go vet ./cmd/miniagent/...
```

---

## Task 8: Agent.RunTurn — the agentic loop

**Files modified**: `cmd/miniagent/agent/agent.go`  
**Depends on**: 7  
**Estimated time**: 12 minutes

### Implementation

Append to `cmd/miniagent/agent/agent.go` (imports already in place from Task 7):

```go
var errMaxStepsReached = errors.New("maximum steps reached — task may be incomplete")

// RunTurn executes one REPL turn (or one-shot task). Appends a user message,
// runs the step loop, and returns nil on success.
func (a *Agent) RunTurn(ctx context.Context, turnID, task string) error {
    // Snapshot for rollback on error (see DESIGN §History rollback)
    snapshot := len(a.messages)
    rollback := func() { a.messages = a.messages[:snapshot] }

    a.messages = a.messages.Append(msg.User(task).Build())

    var stepsCompleted int

    for step := 1; step <= a.maxSteps; step++ {
        // [REVIEW FIX #5]: runStep returns (done, error) — no errContinue sentinel.
        done, err := a.runStep(ctx, turnID, step, &stepsCompleted)
        if err != nil {
            // [REVIEW FIX #4]: always rollback inside the loop.
            // Every error from runStep leaves history in an invalid
            // alternating-role state. errMaxStepsReached is only
            // returned AFTER the loop (no rollback needed there).
            rollback()
            return err
        }
        if done {
            if stepsCompleted > 1 {
                turnRec := a.aggregateTurn(turnID)
                printTurnUsage(a.out, turnID, turnRec)
            }
            return nil
        }
        // done=false, err=nil → model called tools, continue to next step
    }

    // Loop exhausted — no rollback (history ends with assistant message = valid state)
    if stepsCompleted > 1 {
        turnRec := a.aggregateTurn(turnID)
        printTurnUsage(a.out, turnID, turnRec)
    }
    return errMaxStepsReached
}

// runStep executes one LLM call → tool dispatch cycle. Returns:
//   - (true, nil):   turn completed (StopReasonEndTurn or StopReasonMaxTokens)
//   - (false, nil):  model called tools, continue to next step
//   - (_, error):    error — caller should rollback
func (a *Agent) runStep(
    ctx context.Context,
    turnID string,
    step int,
    stepsCompleted *int,
) (done bool, err error) {
    printStepHeader(a.out, step, a.maxSteps)

    // Pass *RequestBuilder directly — it implements Buildable.
    // Provider calls BuildRequest() internally (validates + returns Request).
    rb := llm.NewRequestBuilder().
        Model(a.model).
        MaxTokens(a.maxTokens).
        Append(a.messages...).
        Tools(a.toolDefs...)

    stream, err := a.provider.CreateStream(ctx, rb)
    if err != nil {
        return false, fmt.Errorf("create stream: %w", err)
    }

    // ── Stream processing with live callbacks ──

    sd := newStepDisplay(a.out)
    var stepUsage usage.Record

    result := llm.NewEventProcessor(ctx, stream).
        OnReasoningDelta(func(chunk string) {
            sd.WriteReasoning(chunk)
        }).
        OnTextDelta(func(chunk string) {
            sd.WriteText(chunk)
        }).
        OnEvent(llm.TypedEventHandler[*llm.ToolCallEvent](func(ev *llm.ToolCallEvent) {
            tc := ev.ToolCall
            command, _ := tc.ToolArgs()["command"].(string)
            sd.PrintToolCall(tc.ToolName(), command)
        })).
        OnEvent(llm.TypedEventHandler[*llm.UsageUpdatedEvent](func(ev *llm.UsageUpdatedEvent) {
            rec := ev.Record
            rec.Dims.TurnID = turnID
            a.tracker.Record(rec)
            stepUsage = rec
        })).
        HandleTool(a.toolHandler).
        Result()

    sd.End()

    // ── Display tool results ──
    // [REVIEW FIX #2]: commands already shown live via ToolCallEvent.
    // Only show result lines here — no command duplication.
    for _, tr := range result.ToolResults() {
        output := extractBashOutput(tr.ToolOutput())
        printToolResult(a.out, output, tr.IsError())
    }

    // ── Per-step usage ──

    printStepUsage(a.out, step, stepUsage)

    // ── Branch on stop reason (error paths return before appending to history) ──

    switch result.StopReason() {
    case llm.StopReasonCancelled:
        return false, context.Canceled

    case llm.StopReasonError:
        if rerr := result.Error(); rerr != nil {
            return false, rerr
        }
        return false, errors.New("stream error")
    }

    // ── Append to conversation history (success and tool-use paths only) ──

    a.messages = a.messages.Append(result.Next())
    *stepsCompleted++

    switch result.StopReason() {
    case llm.StopReasonToolUse:
        return false, nil // continue to next step

    case llm.StopReasonMaxTokens:
        fmt.Fprintf(a.out, "\n%s⚠ model hit output token limit%s\n", ansiBrightYellow, ansiReset)
        return true, nil // partial but usable

    default: // StopReasonEndTurn and others
        return true, nil // success
    }
}

// aggregateTurn sums all usage records for a given turn ID.
// TODO: upstream an AggregateRecords([]Record) helper to the usage package.
func (a *Agent) aggregateTurn(turnID string) usage.Record {
    recs := a.tracker.Filter(usage.ByTurnID(turnID), usage.ExcludeEstimates())
    var agg usage.Record
    counts := make(map[usage.TokenKind]int)
    for _, r := range recs {
        for _, item := range r.Tokens {
            counts[item.Kind] += item.Count
        }
        agg.Cost.Total += r.Cost.Total
        agg.Cost.Input += r.Cost.Input
        agg.Cost.Output += r.Cost.Output
        agg.Cost.Reasoning += r.Cost.Reasoning
        agg.Cost.CacheRead += r.Cost.CacheRead
        agg.Cost.CacheWrite += r.Cost.CacheWrite
    }
    for kind, count := range counts {
        agg.Tokens = append(agg.Tokens, usage.TokenItem{Kind: kind, Count: count})
    }
    return agg
}
```

**Verification**:
```bash
go build ./cmd/miniagent/...
go vet ./cmd/miniagent/...
```

---

## Task 9: Agent.RunTurn — tests with fake provider

**Files created**: `cmd/miniagent/agent/agent_test.go`  
**Depends on**: 8  
**Estimated time**: 8 minutes

```go
// cmd/miniagent/agent/agent_test.go
package agent

import (
    "bytes"
    "context"
    "testing"
    "time"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/provider/fake"
    "github.com/codewandler/llm/usage"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// newTestAgent creates an Agent backed by the fake provider.
// Output goes to a buffer (suppresses terminal noise in tests).
func newTestAgent(t *testing.T, opts ...Option) (*Agent, *bytes.Buffer) {
    t.Helper()
    var buf bytes.Buffer
    all := append([]Option{WithOutput(&buf)}, opts...)
    return New(
        fake.NewProvider(),
        t.TempDir(),
        5*time.Second,
        "", // default system prompt
        all...,
    ), &buf
}

// blockingProvider creates a provider whose stream never sends events.
// doProcess can only exit via ctx.Done() → deterministic cancel test.
// Uses llm.NewProvider + StreamFunc: baseProvider.CreateStream just
// delegates to the streamer without model resolution, so this is safe.
func blockingProvider() llm.Provider {
    return llm.NewProvider("blocking",
        llm.WithStreamer(llm.StreamFunc(
            func(_ context.Context, _ llm.Buildable) (llm.Stream, error) {
                ch := make(chan llm.Envelope) // unbuffered, never written to
                return ch, nil
            },
        )),
    )
}

func TestRunTurn_CompletesMultiStep(t *testing.T) {
    // fake provider: call 1 → tool_use (bash "echo hello"), call 2 → text "done"
    a, buf := newTestAgent(t)
    initialMsgs := len(a.messages) // system prompt only

    err := a.RunTurn(context.Background(), "1", "say hello")
    require.NoError(t, err)

    // History grew: system + user + assistant(tool) + tool_result + assistant(text) = 5
    assert.Greater(t, len(a.messages), initialMsgs+1, "messages should grow across steps")

    // Output contains step headers for both steps
    out := buf.String()
    assert.Contains(t, out, "Step 1")
    assert.Contains(t, out, "Step 2")

    // Usage recorded with turnID
    recs := a.Tracker().Filter(usage.ByTurnID("1"))
    assert.NotEmpty(t, recs)
}

func TestRunTurn_MaxStepsReached(t *testing.T) {
    // fake returns tool_use on first call → maxSteps=1 → loop exhausted
    a, _ := newTestAgent(t, WithMaxSteps(1))

    err := a.RunTurn(context.Background(), "1", "do something")
    assert.ErrorIs(t, err, errMaxStepsReached)
}

// [REVIEW FIX #1]: use blocking provider — no buffered events → deterministic cancel.
func TestRunTurn_CancelledContext(t *testing.T) {
    var buf bytes.Buffer
    a := New(blockingProvider(), t.TempDir(), 5*time.Second, "", WithOutput(&buf))

    ctx, cancel := context.WithCancel(context.Background())
    cancel() // cancel before RunTurn

    err := a.RunTurn(ctx, "1", "do something")
    assert.ErrorIs(t, err, context.Canceled)
}

// [REVIEW FIX #1]: use blocking provider for deterministic rollback test.
func TestRunTurn_RollbackOnCancel(t *testing.T) {
    var buf bytes.Buffer
    a := New(blockingProvider(), t.TempDir(), 5*time.Second, "", WithOutput(&buf))
    initialLen := len(a.messages)

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    _ = a.RunTurn(ctx, "1", "do something")
    assert.Equal(t, initialLen, len(a.messages), "messages should be rolled back")
}

func TestRunTurn_NoRollbackOnMaxSteps(t *testing.T) {
    a, _ := newTestAgent(t, WithMaxSteps(1))
    initialLen := len(a.messages)

    _ = a.RunTurn(context.Background(), "1", "do something")
    assert.Greater(t, len(a.messages), initialLen,
        "messages should NOT be rolled back on max-steps (history is valid)")
}

func TestRunTurn_HistoryPersistsAcrossTurns(t *testing.T) {
    a, _ := newTestAgent(t)

    // Turn 1: fake does tool_use → text (2 steps)
    err := a.RunTurn(context.Background(), "1", "first task")
    require.NoError(t, err)
    afterTurn1 := len(a.messages)

    // Turn 2: fake's called flag is true → returns text-only (1 step)
    err = a.RunTurn(context.Background(), "2", "second task")
    require.NoError(t, err)
    afterTurn2 := len(a.messages)

    assert.Greater(t, afterTurn2, afterTurn1, "history should grow across turns")

    // Both turns have usage records
    assert.NotEmpty(t, a.Tracker().Filter(usage.ByTurnID("1")))
    assert.NotEmpty(t, a.Tracker().Filter(usage.ByTurnID("2")))
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run TestRunTurn -v -race
```

---

## Task 10: REPL loop + signal handling + test

**Files modified**: `cmd/miniagent/agent/repl.go`  
**Files created**: `cmd/miniagent/agent/repl_test.go`  
**Depends on**: 8  
**Estimated time**: 10 minutes

### Implementation

```go
// cmd/miniagent/agent/repl.go
package agent

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "sync"
)

// RunREPL runs an interactive prompt loop. Conversation history persists
// across turns. input is the source of user prompts (typically os.Stdin).
// Returns nil on clean exit (EOF / "exit" / "quit").
func RunREPL(ctx context.Context, a *Agent, input io.Reader) error {
    scanner := bufio.NewScanner(input)
    out := a.Out()
    turnID := 0

    // Persistent signal handler — routes SIGINT based on agent state.
    var (
        mu         sync.Mutex
        turnCancel context.CancelFunc
    )
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt)
    // [REVIEW FIX #3]: close(sigCh) after Stop to terminate the goroutine.
    defer func() {
        signal.Stop(sigCh)
        close(sigCh)
    }()

    go func() {
        for range sigCh {
            mu.Lock()
            cancel := turnCancel
            mu.Unlock()
            if cancel != nil {
                cancel() // mid-run: cancel this turn only
            } else {
                // at prompt: print session totals and exit
                fmt.Fprintln(out)
                PrintSessionUsage(out, a.Tracker().Aggregate())
                os.Exit(130)
            }
        }
    }()

    for {
        fmt.Fprint(out, "miniagent> ")
        if !scanner.Scan() {
            break // EOF (Ctrl+D) or read error
        }
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }
        if line == "exit" || line == "quit" {
            break
        }

        turnID++
        turnCtx, cancel := context.WithCancel(ctx)

        mu.Lock()
        turnCancel = cancel
        mu.Unlock()

        err := a.RunTurn(turnCtx, strconv.Itoa(turnID), line)

        mu.Lock()
        turnCancel = nil
        mu.Unlock()
        cancel()

        if err != nil && !errors.Is(err, context.Canceled) {
            printError(out, err)
        }
    }

    fmt.Fprintln(out)
    PrintSessionUsage(out, a.Tracker().Aggregate())
    return nil
}
```

### Test

```go
// cmd/miniagent/agent/repl_test.go
package agent

import (
    "bytes"
    "context"
    "strings"
    "testing"
    "time"

    "github.com/codewandler/llm/provider/fake"
    "github.com/stretchr/testify/assert"
)

func newREPLTestAgent(t *testing.T) (*Agent, *bytes.Buffer) {
    t.Helper()
    var buf bytes.Buffer
    a := New(
        fake.NewProvider(),
        t.TempDir(),
        5*time.Second,
        "",
        WithOutput(&buf),
    )
    return a, &buf
}

func TestRunREPL_ExitCommand(t *testing.T) {
    a, buf := newREPLTestAgent(t)
    input := strings.NewReader("exit\n")

    err := RunREPL(context.Background(), a, input)
    assert.NoError(t, err)
    assert.Contains(t, buf.String(), "session")
}

func TestRunREPL_QuitCommand(t *testing.T) {
    a, buf := newREPLTestAgent(t)
    input := strings.NewReader("quit\n")

    err := RunREPL(context.Background(), a, input)
    assert.NoError(t, err)
    assert.Contains(t, buf.String(), "session")
}

func TestRunREPL_EOF(t *testing.T) {
    a, buf := newREPLTestAgent(t)
    input := strings.NewReader("") // immediate EOF

    err := RunREPL(context.Background(), a, input)
    assert.NoError(t, err)
    assert.Contains(t, buf.String(), "session")
}

func TestRunREPL_ExecutesThenExits(t *testing.T) {
    a, buf := newREPLTestAgent(t)
    input := strings.NewReader("say hello\nexit\n")

    err := RunREPL(context.Background(), a, input)
    assert.NoError(t, err)

    out := buf.String()
    assert.Contains(t, out, "Step 1")
    assert.Contains(t, out, "session")
}

func TestRunREPL_SkipsEmptyLines(t *testing.T) {
    a, buf := newREPLTestAgent(t)
    input := strings.NewReader("\n\n  \nexit\n")

    err := RunREPL(context.Background(), a, input)
    assert.NoError(t, err)
    assert.NotContains(t, buf.String(), "Step 1")
}
```

**Verification**:
```bash
go test ./cmd/miniagent/agent/ -run TestRunREPL -v
```

---

## Task 11: main.go — cobra command, provider setup, flag wiring

**Files modified**: `cmd/miniagent/main.go`  
**Depends on**: 8, 10  
**Estimated time**: 8 minutes

### Implementation

```go
// cmd/miniagent/main.go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "path/filepath"
    "time"

    "github.com/spf13/cobra"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/cmd/llmcli/store"
    "github.com/codewandler/llm/cmd/miniagent/agent"
    "github.com/codewandler/llm/provider/auto"
)

func main() {
    if err := rootCmd().Execute(); err != nil {
        // SilenceErrors is true, so cobra won't print the error.
        // Print it ourselves so the user sees what went wrong.
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}

func rootCmd() *cobra.Command {
    var (
        model        string
        workspace    string
        maxSteps     int
        maxTokens    int
        systemPrompt string
        timeout      time.Duration
    )

    cmd := &cobra.Command{
        Use:   "miniagent [task]",
        Short: "A minimal agentic CLI with a bash tool",
        Long: `miniagent runs an autonomous agent loop: LLM → bash → LLM → ...

With no arguments it starts an interactive REPL.
With a positional argument it runs the task once and exits.`,
        Args:          cobra.MaximumNArgs(1),
        SilenceUsage:  true,
        SilenceErrors: true,
        RunE: func(_ *cobra.Command, args []string) error {
            return execute(args, model, workspace, maxSteps, maxTokens, systemPrompt, timeout)
        },
    }

    f := cmd.Flags()
    f.StringVarP(&model, "model", "m", "default", "Model alias or full path")
    f.StringVarP(&workspace, "workspace", "w", "", "Working directory (default: $PWD)")
    f.IntVar(&maxSteps, "max-steps", 30, "Maximum agent loop iterations per turn")
    f.IntVar(&maxTokens, "max-tokens", 16_000, "Maximum output tokens per LLM call")
    f.StringVarP(&systemPrompt, "system", "s", "", "Override the system prompt body")
    f.DurationVar(&timeout, "timeout", 30*time.Second, "Per-command bash timeout")

    return cmd
}

func execute(
    args []string,
    model, workspace string,
    maxSteps, maxTokens int,
    systemPrompt string,
    timeout time.Duration,
) error {
    // Resolve and validate workspace
    if workspace == "" {
        wd, err := os.Getwd()
        if err != nil {
            return fmt.Errorf("get working directory: %w", err)
        }
        workspace = wd
    }
    workspace, _ = filepath.Abs(workspace)
    if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
        return fmt.Errorf("workspace directory does not exist: %s", workspace)
    }

    // Provider setup (mirrors cmd/llmcli)
    ctx := context.Background()
    provider, err := createProvider(ctx)
    if err != nil {
        return err
    }

    // Build agent
    a := agent.New(provider, workspace, timeout, systemPrompt,
        agent.WithModel(model),
        agent.WithMaxSteps(maxSteps),
        agent.WithMaxTokens(maxTokens),
    )

    // One-shot mode
    if len(args) == 1 {
        ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
        defer cancel()
        err := a.RunTurn(ctx, "1", args[0])
        fmt.Println()
        agent.PrintSessionUsage(os.Stdout, a.Tracker().Aggregate())
        if err != nil {
            return err
        }
        return nil
    }

    // REPL mode
    return agent.RunREPL(ctx, a, os.Stdin)
}

// [REVIEW FIX #6]: return llm.Provider interface, not *router.Provider.
func createProvider(ctx context.Context) (llm.Provider, error) {
    var autoOpts []auto.Option
    autoOpts = append(autoOpts, auto.WithName("miniagent"))

    // Claude OAuth token store — non-fatal if unavailable
    if dir, err := store.DefaultDir(); err == nil {
        if ts, err := store.NewFileTokenStore(dir); err == nil {
            autoOpts = append(autoOpts, auto.WithClaude(ts))
        }
    }

    provider, err := auto.New(ctx, autoOpts...)
    if err != nil {
        return nil, fmt.Errorf(`no LLM providers found.

Set one of:
  ANTHROPIC_API_KEY    — Anthropic direct API
  OPENAI_API_KEY       — OpenAI
  OPENROUTER_API_KEY   — OpenRouter

Or authenticate with Claude:
  llmcli auth login

(%w)`, err)
    }
    return provider, nil
}
```

**Verification**:
```bash
go build ./cmd/miniagent/...
go vet ./cmd/miniagent/...
go build -o /dev/null ./cmd/miniagent/
```

---

## Task 12: End-to-end verification

**Estimated time**: 5 minutes

### Automated checks

```bash
# All tests pass (with race detector)
go test ./cmd/miniagent/... -v -count=1 -race

# Build clean
go build -o /dev/null ./cmd/miniagent/...

# Vet passes
go vet ./cmd/miniagent/...

# Format check (no output = clean)
gofmt -l ./cmd/miniagent/

# Imports check
goimports -l ./cmd/miniagent/ 2>/dev/null || true
```

### Manual smoke test (if credentials available)

```bash
# One-shot
go run ./cmd/miniagent "echo hello world"

# REPL: type a task, verify step display, type "exit"
go run ./cmd/miniagent
```

### Acceptance checklist

- [ ] `miniagent "echo hello"` → completes, shows usage, exits 0
- [ ] `miniagent` → shows `miniagent>` prompt
- [ ] Task in REPL → step headers, tool calls with `🔧`, results with `✓`/`✗`, usage
- [ ] Multi-call step → no command duplication (calls shown live, results shown after)
- [ ] Second task → conversation history preserved
- [ ] `exit` / `quit` / Ctrl+D → session totals printed
- [ ] Per-step usage after every LLM call
- [ ] Per-turn summary only when steps > 1
- [ ] Ctrl+C mid-run → cancels turn, re-prompts
- [ ] `--max-steps 1` → errMaxStepsReached
- [ ] Non-zero bash exit → fed back as tool content (model sees it)
- [ ] `go build`, `go vet`, `go test -race` all clean
- [ ] Ctrl+C at prompt → prints session totals, exits 130
- [ ] Turn error before any successful step → user message rolled back from history
- [ ] No providers available → clear error message with setup instructions
- [ ] Non-existent `--workspace` → error on startup before agent construction

---

## Summary

| Task | Description | Files | Min | Depends on |
|------|-------------|-------|-----|------------|
| 1 | Scaffold stubs | 6 new | 2 | — |
| 2 | System prompt + test | system.go, system_test.go | 4 | 1 |
| 3 | Truncation + test | tools.go, tools_test.go | 4 | 1 |
| 4 | Bash handler + test | tools.go, tools_test.go | 8 | 3 |
| 5 | Format helpers + test | display.go, display_test.go | 8 | 1 |
| 6 | Step display + usage + test | display.go, display_test.go | 10 | 5 |
| 7 | Agent struct + constructor | agent.go | 5 | 2, 4, 6 |
| 8 | RunTurn loop | agent.go | 12 | 7 |
| 9 | RunTurn tests | agent_test.go | 8 | 8 |
| 10 | REPL + signal + test | repl.go, repl_test.go | 10 | 8 |
| 11 | main.go wiring | main.go | 8 | 8, 10 |
| 12 | E2E verification | — | 5 | 11 |
| | **Total** | | **~84** | |

---

## Review Fixes Applied

| # | Issue | Fix applied in |
|---|-------|----------------|
| 1 | Flaky cancel test — race with buffered events | Task 9: `blockingProvider()` with unbuffered never-written channel |
| 2 | Multi-call display duplication | Task 6: `printToolResult` takes `(output, isError)` only — no command param. Task 8: result loop simplified |
| 3 | REPL goroutine leak | Task 10: `defer func() { signal.Stop(sigCh); close(sigCh) }()` |
| 4 | `shouldRollback` dead code | Task 8: removed; always `rollback()` in the loop error path |
| 5 | `errContinue` sentinel anti-pattern | Task 8: `runStep` returns `(done bool, err error)` |
| 6 | `createProvider` returns concrete type | Task 11: returns `llm.Provider` interface |

## Self-Review Fixes (v2)

| # | Issue | Fix applied in |
|---|-------|----------------|
| 7 | Errors silenced in one-shot mode (`SilenceErrors: true` + no print) | Task 11: `main()` prints error to stderr before `os.Exit(1)` |
| 8 | Step header box misaligns with emoji + variable widths | Task 6: replaced box with simple `── 💭 Step N/M ────...` rule line |
| 9 | No test for 20KB output truncation cap | Task 3: added `TestTruncateBytes_LargeOutput` |
| 10 | Import merge ambiguity between Task 7 and Task 8 | Task 7: import block includes all imports for both tasks; Task 8 note says "imports already in place" |
