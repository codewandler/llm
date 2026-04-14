# DESIGN: cmd/miniagent — Agentic CLI

**Date**: 2026-05-14  
**Status**: Refined v4  
**Scope**: `cmd/miniagent/` only — no changes to core library packages

---

## Overview

`miniagent` is a minimal CLI agent that runs an autonomous agentic loop: it sends a task to an LLM, executes any tool calls the model requests, feeds results back, and repeats until the model produces a response with no tool calls (task complete) or a step limit is reached.

It mirrors the core loop of [MiniMax Mini-Agent](https://github.com/MiniMax-AI/Mini-Agent) but:
- Is written in Go and lives inside this repository as `cmd/miniagent`
- Uses the existing `llm` library (`provider/auto`, `StreamProcessor`, `tool`, `usage.Tracker`, etc.)
- Implements **only one tool**: `bash` — execute a shell command and return stdout/stderr
- Supports both **one-shot** (task as positional arg) and **REPL** (interactive multi-turn) modes

---

## User-Facing Interface

### Modes

**REPL (default — no positional arg):**
```
$ miniagent
miniagent> create a file called test.txt with "hello" and verify it
...agent runs multiple steps...
miniagent> now uppercase it
...agent continues with conversation history...
miniagent> exit
```

**One-shot (positional arg):**
```
$ miniagent "list all Go files and count lines of code"
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--model`, `-m` | `default` | Model alias or full path (routed by `provider/auto`) |
| `--workspace`, `-w` | `$PWD` | Working directory for bash commands |
| `--max-steps` | `30` | Maximum agent loop iterations per turn |
| `--max-tokens` | `16000` | Maximum output tokens per LLM call |
| `--system`, `-s` | (built-in) | Override the system prompt body |
| `--timeout` | `30s` | Per-command bash timeout |

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Task completed (one-shot) or REPL exited cleanly |
| 1 | Task failed (max steps, provider error, etc.) |
| 130 | Interrupted by SIGINT at prompt |

---

## Architecture

### Package layout

```
cmd/miniagent/
├── main.go          # Entry point, cobra root command, provider setup
└── agent/
    ├── agent.go     # Agent struct, RunTurn loop, streaming + usage wiring
    ├── display.go   # Terminal output (colors, step boxes, usage, formatting)
    ├── tools.go     # bash tool: spec, handler, output truncation
    ├── system.go    # system prompt builder
    └── repl.go      # REPL loop, prompt, signal routing
```

### Dependencies from this repository

| Package | What we use |
|---------|-------------|
| `llm` | `Provider`, `Request`, `NewRequestBuilder`, `NewEventProcessor`, `StopReason*`, event types |
| `llm/tool` | `NewSpec`, `NewHandler`, `Definition`, `NamedHandler` |
| `llm/msg` | `Messages`, `User()`, `System()`, `CacheTTL1h` |
| `llm/usage` | `Tracker`, `NewTracker`, `WithCostCalculator`, `Default()`, `Record`, `TokenItems`, token kinds |
| `provider/auto` | `auto.New(ctx)` |
| `cmd/llmcli/store` | `FileTokenStore` (Claude OAuth credentials) |

### External dependencies

None new. Uses `cobra` (already in go.mod) and stdlib `bufio.Scanner` for the REPL.

---

## Agent (agent.go)

### Struct

```go
type Agent struct {
    provider    llm.Provider
    messages    msg.Messages         // conversation history — grows across REPL turns
    tracker     *usage.Tracker       // accumulates all records, tagged by turn
    toolDefs    []tool.Definition    // [bash definition]
    toolHandler tool.NamedHandler    // bash handler
    model       string               // model alias or full path
    maxSteps    int
    maxTokens   int
}
```

### Constructor

```go
func New(provider llm.Provider, opts ...Option) *Agent
```

Options: `WithModel(string)`, `WithWorkspace(string)`, `WithMaxSteps(int)`, `WithMaxTokens(int)`, `WithTimeout(time.Duration)`, `WithSystemPrompt(string)`.

The tracker is created internally with `usage.Default()` as fallback calculator:
```go
tracker := usage.NewTracker(
    usage.WithCostCalculator(usage.Default()),
)
```

The system prompt (from `system.go`) is the first message in `agent.messages`, created once during construction with `CacheTTL1h`:
```go
agent.messages = msg.Messages{
    msg.System(systemPrompt).Cache(msg.CacheTTL1h).Build(),
}
```

This ensures subsequent REPL turns benefit from Anthropic's prompt caching (cache reads instead of full input processing).

### RunTurn(ctx, turnID, task) error

Called once per REPL turn (or once for one-shot mode). Returns nil on success.

```
0. Snapshot history for rollback:
     snapshot := len(agent.messages)

1. Append user message:
     agent.messages = agent.messages.Append(msg.User(task).Build())

2. Step loop (step = 1..maxSteps):

     a. Display step header:  "💭 Step {step}/{maxSteps}"

     b. Build request (from scratch — messages grow each step):
          req, err := llm.NewRequestBuilder().
              Model(agent.model).
              MaxTokens(agent.maxTokens).
              Append(agent.messages...).
              Tools(agent.toolDefs...).
              Build()
          if err → rollback & return err

     c. Create stream:
          stream, err := agent.provider.CreateStream(ctx, req)
          if err → rollback & return err

     d. Process stream with real-time callbacks:

          stepDisplay := newStepDisplay()
          var stepUsage usage.Record

          result := llm.NewEventProcessor(ctx, stream).
              OnReasoningDelta(func(chunk string) {
                  stepDisplay.WriteReasoning(chunk)       // dim text, live
              }).
              OnTextDelta(func(chunk string) {
                  stepDisplay.WriteText(chunk)             // normal text, live
              }).
              OnEvent(llm.TypedEventHandler[*llm.ToolCallEvent](func(ev) {
                  stepDisplay.PrintToolCall(ev.ToolCall)   // 🔧 bash + $ cmd
              })).
              OnEvent(llm.TypedEventHandler[*llm.UsageUpdatedEvent](func(ev) {
                  rec := ev.Record
                  rec.Dims.TurnID = turnID
                  agent.tracker.Record(rec)
                  stepUsage = rec
              })).
              HandleTool(agent.toolHandler).
              Result()

     e. After Result() returns (tool handlers have executed):
          stepDisplay.End()

          // Show tool results paired with their calls
          for i, tr := range result.ToolResults() {
              stepDisplay.PrintToolResult(result.ToolCalls()[i], tr)
          }

          // Show per-step usage (dim)
          printStepUsage(step, stepUsage)

     f. Append result to conversation history:
          agent.messages = agent.messages.Append(result.Next())

     g. Branch on stop reason:

          StopReasonToolUse:
              → continue loop (next step)

          StopReasonEndTurn:
              → if steps > 1: print turn usage summary
              → return nil  ✓ success

          StopReasonCancelled:
              → rollback
              → return context.Canceled

          StopReasonError:
              → rollback
              → return result.Error()

          StopReasonMaxTokens:
              → print warning "model hit output token limit"
              → return nil  (partial response is still usable)

3. Loop exhausted:
     → print turn usage summary
     → return errMaxStepsReached  (partial conversation kept — not rolled back)
```

### History rollback

```go
rollback := func() { agent.messages = agent.messages[:snapshot] }
```

**When to rollback**: any error that prevents the turn from producing a valid conversation exchange (cancelled, CreateStream failure, provider error). The goal is to keep `agent.messages` in a state where the next turn can append a user message without violating the provider's alternating-role requirement.

**Why this matters**: Anthropic's API requires strict user↔assistant alternation. Tool results are serialized as `role: "user"` messages. If a turn errors mid-step (e.g. after step 1 produced assistant+tool_results but step 2's CreateStream failed), the history ends with a tool_results message (`role: "user"` in the API). Appending a new user message would create consecutive user messages → API rejection.

Rollback restores history to the end of the previous *turn's* successful exchange, which always ends with an assistant message (valid state for a new user message).

**When NOT to rollback**:
- `StopReasonEndTurn` — turn completed successfully
- `StopReasonMaxTokens` — partial but usable; the assistant message is appended, history is valid
- `errMaxStepsReached` — partial conversation kept; the last step has an assistant message, so history is valid for the next turn

### Multiple tool calls per step

The model may emit multiple tool calls in a single response (e.g. two bash commands). The library handles this correctly:

1. During streaming: each `ToolCallEvent` fires → `stepDisplay.PrintToolCall()` shows each call live
2. After stream ends: `HandleTool` dispatches all calls via sync dispatcher (sequential, in order)
3. After `Result()`: `ToolCalls()` and `ToolResults()` are 1:1 by index

Display pairs them:

```
🔧 bash
   $ echo "hello" > test.txt
🔧 bash
   $ cat test.txt

✓ $ echo "hello" > test.txt
  (no output)
✓ $ cat test.txt
  hello
```

When there's only one call (the common case), the paired display simplifies naturally:

```
🔧 bash
   $ echo "hello" > test.txt

✓ (no output)
```

---

## Event Timing

```
stream events arrive ────────────────────────────────────┐
│                                                         │
│  DeltaEvent(thinking)  → OnReasoningDelta → dim output │  ← real-time
│  DeltaEvent(text)      → OnTextDelta → normal output   │  ← real-time
│  ToolCallEvent[0]      → OnEvent → 🔧 bash + $ cmd    │  ← real-time
│  ToolCallEvent[1]      → OnEvent → 🔧 bash + $ cmd    │  ← real-time (if multi-call)
│  UsageUpdatedEvent     → OnEvent → tracker.Record      │  ← recorded
│  CompletedEvent        → stop reason set               │
│                                                         │
channel closes ──────────────────────────────────────────┘
│
│  dispatchToolCalls() runs:                             ← bash commands execute here
│    sync: call[0] executes → result[0]                      (sequential, in order)
│          call[1] executes → result[1]
│
Result() returns ────────────────────────────────────────
│
│  display tool results paired with calls (✓/✗)          ← shown after execution
│  display step usage line
│  append assistant + tool results to message history
│  decide: continue loop or end turn
```

---

## REPL (repl.go)

### Loop

```go
func RunREPL(ctx context.Context, agent *Agent) error {
    scanner := bufio.NewScanner(os.Stdin)
    turnID := 0

    // Persistent signal handler — routes SIGINT based on agent state
    var (
        mu         sync.Mutex
        turnCancel context.CancelFunc   // non-nil while a turn is running
    )
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt)
    go func() {
        for range sigCh {
            mu.Lock()
            cancel := turnCancel
            mu.Unlock()
            if cancel != nil {
                cancel()                                       // mid-run: cancel only this turn
            } else {
                printSessionUsage(agent.tracker.Aggregate())   // at prompt: print totals and exit
                os.Exit(130)
            }
        }
    }()

    for {
        fmt.Print("miniagent> ")
        if !scanner.Scan() {
            break   // EOF (Ctrl+D) or read error
        }
        line := strings.TrimSpace(scanner.Text())
        if line == "" { continue }
        if line == "exit" || line == "quit" { break }

        turnID++
        turnCtx, cancel := context.WithCancel(ctx)

        mu.Lock()
        turnCancel = cancel
        mu.Unlock()

        err := agent.RunTurn(turnCtx, strconv.Itoa(turnID), line)

        mu.Lock()
        turnCancel = nil
        mu.Unlock()
        cancel()

        if err != nil && !errors.Is(err, context.Canceled) {
            printError(err)
        }
    }

    printSessionUsage(agent.tracker.Aggregate())
    return nil
}
```

### Signal behaviour

| State | SIGINT does | Why |
|---|---|---|
| At `miniagent> ` prompt | Print session totals → `os.Exit(130)` | Standard terminal behaviour |
| `RunTurn` executing | `turnCancel()` → cancel turn context only | Interrupts model call or bash execution; REPL re-prompts |
| After Ctrl+C cancel | REPL re-prompts; history rolled back | Turn's partial messages removed; clean state for next prompt |

A single persistent goroutine reads from `sigCh`. It checks `turnCancel` under a mutex to decide which path to take.

### One-shot mode (main.go)

```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
defer cancel()
err := agent.RunTurn(ctx, "1", task)
printSessionUsage(agent.tracker.Aggregate())
```

---

## Bash Tool (tools.go)

### Spec

```go
type BashParams struct {
    Command string `json:"command" jsonschema:"description=Shell command to execute,required"`
}

var BashSpec = tool.NewSpec[BashParams]("bash",
    "Execute a bash command in the workspace directory. Returns combined stdout and stderr.",
)
```

### Handler

```go
const maxOutputBytes = 20 * 1024  // 20 KB

type BashResult struct {
    Output string `json:"output"`
}

func NewBashHandler(workspace string, timeout time.Duration) tool.NamedHandler {
    return tool.NewHandler("bash", func(ctx context.Context, in BashParams) (*BashResult, error) {
        cmdCtx, cancel := context.WithTimeout(ctx, timeout)
        defer cancel()

        cmd := exec.CommandContext(cmdCtx, "bash", "-c", in.Command)
        cmd.Dir = workspace
        out, err := cmd.CombinedOutput()

        output := truncateBytes(out, maxOutputBytes)

        if cmdCtx.Err() == context.DeadlineExceeded {
            return &BashResult{Output: fmt.Sprintf("timeout: command exceeded %s", timeout)}, nil
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
    })
}
```

### Behaviour contracts

| Scenario | Tool result content | Go error |
|---|---|---|
| Command succeeds (exit 0) | stdout+stderr | `nil` |
| Non-zero exit | `exit N:\n` + stdout+stderr | `nil` |
| Timeout | `timeout: command exceeded Ns` | `nil` |
| exec failure (bad binary) | `error: exec: "xyz": not found` | `nil` |
| Context cancelled (SIGINT) | partial output or empty | `nil` |

The handler **never** returns a Go error. Every outcome is tool result content that the model sees and can react to.

### Output truncation

```go
func truncateBytes(b []byte, max int) string {
    if len(b) <= max {
        return string(b)
    }
    return string(b[:max]) + fmt.Sprintf("\n[...truncated, showing first %d of %d bytes]", max, len(b))
}
```

Two-tier truncation:
| Layer | Limit | Purpose |
|---|---|---|
| Tool result (model sees) | 20 KB | Prevent context window flooding |
| Terminal display | 300 chars | Keep display clean |

---

## Display (display.go)

### Streaming state machine

A `stepDisplay` struct manages ANSI output state within a single step:

```go
type stepDisplay struct {
    state      displayState
    toolIndex  int    // number of tool calls shown this step
}

type displayState int
const (
    stateIdle      displayState = iota
    stateReasoning                     // emitting dim reasoning tokens
    stateText                          // emitting normal text tokens
)
```

**WriteReasoning(chunk)**

| From | Action |
|---|---|
| idle | emit `\033[2m` (dim on); set state=reasoning |
| reasoning | *(no transition)* |
| text | *(shouldn't happen — reasoning precedes text)* |

Print chunk to stdout.

**WriteText(chunk)**

| From | Action |
|---|---|
| idle | emit newline; set state=text |
| reasoning | emit `\033[0m` (reset) + newline; set state=text |
| text | *(no transition)* |

Print chunk to stdout.

**PrintToolCall(tc)**

| From | Action |
|---|---|
| idle | *(no transition needed)* |
| reasoning | emit `\033[0m\n` |
| text | emit `\n` |

Print `🔧 bash\n   $ {command}\n`. Increment toolIndex.

**End()**

| From | Action |
|---|---|
| reasoning | emit `\033[0m\n` |
| text | emit `\n` |
| idle | *(nothing)* |

Set state=idle. Called after `Result()` returns, before tool results display.

### Terminal elements with worked example

Full output for a two-step, single-turn session:

```
miniagent> create test.txt with "hello" and verify it

╭──────────────────────────────────────────╮
│ 💭 Step 1/30                              │
╰──────────────────────────────────────────╯
I'll create the file and verify it.                          ← streamed text (normal)

🔧 bash                                                     ← ToolCallEvent (live, before execution)
   $ echo "hello" > test.txt
🔧 bash                                                     ← second ToolCallEvent
   $ cat test.txt
                                                             ← bash commands execute here
✓ $ echo "hello" > test.txt                                 ← paired result display
  (no output)
✓ $ cat test.txt                                            ← paired result display
  hello

   ── step 1 ── input: 1 204  cache_read: 8 432  output: 87  cost: $0.0023

╭──────────────────────────────────────────╮
│ 💭 Step 2/30                              │
╰──────────────────────────────────────────╯
Done! I created `test.txt` with "hello" and verified        ← streamed text (normal)
the contents match.

   ── step 2 ── input: 2 408  cache_read: 8 432  output: 42  cost: $0.0018
   ── turn 1 ── input: 3 612  cache_read: 16 864  output: 129  cost: $0.0041
```

A single-step turn (no tool calls — e.g. a knowledge question):

```
miniagent> what is a goroutine?

╭──────────────────────────────────────────╮
│ 💭 Step 1/30                              │
╰──────────────────────────────────────────╯
A goroutine is a lightweight thread of execution...          ← streamed text

   ── step 1 ── input: 892  output: 214  cost: $0.0012
```

No per-turn summary when there's only one step (it would duplicate the step line).

A step with reasoning:

```
╭──────────────────────────────────────────╮
│ 💭 Step 3/30                              │
╰──────────────────────────────────────────╯
The user wants me to fix the build error. Let me check...   ← dim (reasoning)

The build is failing because of a missing import.           ← normal (text, after reasoning)

🔧 bash
   $ head -20 main.go
```

### Tool result display

**Single tool call** (common case): just show the result.
```
✓ hello world
```

**Multiple tool calls**: pair each result with its command for clarity.
```
✓ $ echo "hello" > test.txt
  (no output)
✓ $ cat test.txt
  hello
```

**Error result**:
```
✗ exit 1:
  bash: foobar: command not found
```

**Empty output** (exit 0, no stdout/stderr):
```
✓ (no output)
```

**Long result** (display-truncated at 300 chars; model sees full 20 KB):
```
✓ package main\n\nimport (\n\t"fmt"\n)\n\nfunc main() {\n\tfmt.Pr...
```

### Usage display

**Per-step** (dim, after every step):
```
   ── step 2 ── input: 1 204  cache_read: 8 432  output: 87  cost: $0.0023
```

**Per-turn** (only when steps > 1):
```
   ── turn 1 ── input: 3 612  cache_read: 16 864  output: 129  cost: $0.0041
```

**Session total** (on exit):
```
── session ── input: 7 224  cache_read: 33 728  output: 475  cost: $0.0131
```

### Formatting rules

- **Token counts**: thin-space (`\u2009`) thousands separator (e.g. `8 432`)
- **Cost**: adaptive precision — `$0.0023` (< $0.01), `$0.0412` (< $1), `$1.24` (≥ $1). Omitted entirely when `Cost.IsZero()`.
- **Token kinds shown** (only when count > 0): `input`, `cache_r`, `cache_w`, `output`, `reason`
- **Ordering**: input → cache_r → cache_w → output → reason → cost

### Color reference

| Element | ANSI |
|---|---|
| Step box border | `\033[2m` (dim) |
| Step number | `\033[1m\033[96m` (bold + bright cyan) |
| `🔧 bash` | `\033[93m` (bright yellow) |
| `$ command` | `\033[2m` (dim) |
| `✓` | `\033[92m` (bright green) |
| `✗` | `\033[91m` (bright red) |
| Reasoning text | `\033[2m` (dim) |
| Usage lines | `\033[2m` (dim) |
| Session total | default (no ANSI) |

---

## System Prompt (system.go)

```go
func BuildSystemPrompt(workspace string) string
```

Default:

```
You are a helpful terminal assistant. You complete tasks by running bash commands.
Think step by step. When the task is done, respond with a clear summary of what you accomplished.
Do not ask for confirmation — just proceed with the task.

## Workspace
You are working in: {abs_workspace}
All relative paths resolve from this directory.
```

When `--system` is provided, the user's text replaces the body above the `## Workspace` section. The workspace section is always appended — the model always needs to know the working directory.

---

## Provider Setup (main.go)

Mirrors `cmd/llmcli/cmds/shared.go`:

```go
dir, err := store.DefaultDir()
tokenStore, err := store.NewFileTokenStore(dir)
provider, err := auto.New(ctx,
    auto.WithName("miniagent"),
    auto.WithClaude(tokenStore),
)
```

Zero-config provider detection: Claude OAuth → Anthropic API key → Bedrock → OpenAI → OpenRouter.

If `auto.New` returns `router.ErrNoProviders`, print a clear message:

```
Error: no LLM providers found.

Set one of:
  ANTHROPIC_API_KEY    — Anthropic direct API
  OPENAI_API_KEY       — OpenAI
  OPENROUTER_API_KEY   — OpenRouter

Or authenticate with Claude:
  llmcli auth login
```

Validate `--workspace` exists before constructing the agent:

```go
if _, err := os.Stat(workspace); err != nil {
    return fmt.Errorf("workspace directory does not exist: %s", workspace)
}
```

---

## Error Handling

### In RunTurn

| Error source | Behaviour | Rollback? |
|---|---|---|
| `Request.Build()` fails | Return error; REPL re-prompts | Yes |
| `CreateStream()` fails (network, rate limit) | Return error; REPL re-prompts | Yes |
| `StopReasonError` (provider error event mid-stream) | Return `result.Error()`; partial streamed text visible | Yes |
| `StopReasonCancelled` (SIGINT) | Return `context.Canceled`; REPL re-prompts | Yes |
| `StopReasonMaxTokens` (output truncated) | Print warning; return nil; treat as usable partial | No |
| `errMaxStepsReached` (loop exhausted) | Print warning; return error | No |

"Rollback" means `agent.messages = agent.messages[:snapshot]` — restoring history to the end of the previous turn.

### In REPL

```go
if err != nil && !errors.Is(err, context.Canceled) {
    printError(err)
}
```

Errors are printed; the REPL continues. Cancelled turns silently re-prompt.

### Context overflow

When the conversation history exceeds the model's context window, `CreateStream` returns a provider error (e.g. Anthropic `400: max tokens exceeded`). This triggers rollback + error display:

```
Error: anthropic: 400 — prompt is too long (estimated 210,000 tokens, max 200,000).
Start a new session or use a model with a larger context window.
```

The user can `exit` and restart, or try a shorter prompt (history was rolled back).

---

## Key Design Decisions

### 1. Streaming tokens via OnTextDelta/OnReasoningDelta
Text and reasoning stream live to stdout. A `stepDisplay` state machine manages ANSI transitions (dim→normal). No headers — reasoning flows as dim text; normal text flows after. The step box frames context.

### 2. Tool calls shown live via ToolCallEvent; results shown after execution
`OnEvent(ToolCallEvent)` fires when the stream delivers a completed tool call — *before* `HandleTool` dispatches. The user sees `🔧 bash  $ command` then a natural pause while bash runs, then `✓ result`. Feels like watching the agent work.

### 3. Paired tool call/result display for multi-call steps
When the model emits multiple tool calls in one response, all are displayed live as they arrive. Results are displayed after all handlers finish, paired with their commands. Single-call steps (the common case) simplify naturally.

### 4. History rollback on turn error
On any error that prevents a complete turn exchange, `agent.messages` is restored to pre-turn state. This prevents consecutive `role: "user"` messages (Anthropic serializes tool results as `role: "user"`; a dangling tool result + new user message = API rejection).

### 5. CacheTTL1h on system prompt
The system message carries `msg.CacheTTL1h`. First turn pays full input cost; subsequent turns get cache reads on the system prompt prefix.

### 6. Providers already compute Cost on UsageUpdatedEvent
Anthropic/Claude/Bedrock/OpenAI compute `Cost` via `usage.Default()` before emitting. The tracker's `WithCostCalculator(usage.Default())` is a fallback for providers that don't.

### 7. TurnID stamped in OnEvent handler
`OnEvent(UsageUpdatedEvent)` copies `ev.Record`, sets `rec.Dims.TurnID`, calls `tracker.Record(rec)`. We mutate our local copy, not the library's event.

### 8. Non-zero exit is tool content, not a Go error
The bash handler always returns `(result, nil)`. All outcomes are content strings the model sees.

### 9. Two-tier output truncation
Model sees up to 20 KB (with `[...truncated]` marker). Terminal shows up to 300 chars. Model has the context it needs; human sees a preview.

### 10. Persistent SIGINT goroutine with mutex-guarded turnCancel
One goroutine for the REPL lifetime. Checks `turnCancel` under `sync.Mutex`: non-nil → cancel turn; nil → print session totals and `os.Exit(130)`.

### 11. Per-turn usage only when steps > 1
Single-step turns: step line already shows everything. Per-turn summary is redundant and skipped.

### 12. MaxTokens default 16000
Agent use cases produce longer outputs than single-shot inference. Covers multi-step reasoning without prematurely hitting output limits.

### 13. Request rebuilt from scratch each step
Each step sends the full conversation via `NewRequestBuilder().Append(agent.messages...)`. This is correct — the API is stateless. Cache hints mitigate cost growth.

### 14. No rollback on max-steps or max-tokens
The conversation history from partial turns is valid (ends with an assistant message). The next turn can continue naturally.

---

## Out of Scope

- Context summarisation / token limit management
- Session notes / persistent memory / session save/restore
- File system tools (read_file, write_file, list_files)
- MCP tool integration
- Token estimation / drift display
- Budget enforcement (`usage.Budget`)
- Readline / line editing / command history
- `--verbose` / `--log-events` debug flags
- Safety guardrails for bash commands (user is responsible)

---

## Acceptance Criteria

1. `miniagent "echo hello"` completes in one step, streams output, prints session usage, exits 0.
2. `miniagent` starts a REPL; typing a task runs the agent; typing another task continues with full conversation history.
3. Reasoning tokens stream in dim; text tokens stream in normal weight.
4. `🔧 bash` + command appear *before* execution; `✓`/`✗` + result appear *after*.
5. Multiple tool calls in one response are displayed and executed correctly.
6. A multi-step task completes with correct tool result threading across steps.
7. Non-zero bash exit is fed back to the model as tool content.
8. Per-step usage line appears after every LLM call; per-turn summary only when steps > 1.
9. Session total appears on REPL exit or one-shot completion.
10. Usage lines show cache_read/cache_write only when non-zero; cost omitted when unavailable.
11. Ctrl+C mid-run cancels the turn (history rolled back) and re-prompts.
12. Ctrl+C at prompt prints session totals and exits 130.
13. Ctrl+D or `exit`/`quit` at prompt prints session totals and exits 0.
14. Turn error before any successful step rolls back the user message from history.
15. `--max-steps 1` stops after one LLM call.
16. No providers available → clear error message with setup instructions.
17. Non-existent `--workspace` → error on startup.
18. `go build ./cmd/miniagent/...` and `go vet ./cmd/miniagent/...` pass clean.
