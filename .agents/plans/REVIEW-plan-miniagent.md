# REVIEW: PLAN-miniagent.md

**Date**: 2026-05-14  
**Reviewer**: Agent  
**Verdict**: Mostly solid. Six issues need fixing before implementation.

---

## ❌ Issues to Fix

### 1. Flaky cancel test (Task 9 — `TestRunTurn_CancelledContext`)

**Problem**: The fake provider publishes to a buffered channel (capacity 64). By the time `doProcess` starts, all events are already in the buffer. With a pre-cancelled context, `doProcess` enters:

```go
select {
case <-r.ctx.Done():      // ready — context already cancelled
case ev, ok := <-r.ch:    // ready — buffered events waiting
}
```

Go's select picks randomly. The test might process all events (stop reason = `StopReasonToolUse`) instead of cancelling. The test asserts `errors.Is(err, context.Canceled)` — this will fail intermittently.

**Fix**: Don't use the fake provider for this test. Use a blocking stream that never sends events:

```go
func TestRunTurn_CancelledContext(t *testing.T) {
    // blockingStream never sends events — ctx.Done() is the only select branch
    ch := make(chan llm.Envelope)
    provider := llm.NewProviderFromStreamFunc("blocking", func(ctx context.Context, _ llm.Buildable) (llm.Stream, error) {
        return ch, nil
    })
    a := New(provider, t.TempDir(), 5*time.Second, "", WithOutput(&bytes.Buffer{}))

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    err := a.RunTurn(ctx, "1", "do something")
    assert.ErrorIs(t, err, context.Canceled)
}
```

Same fix for `TestRunTurn_RollbackOnCancel`.

---

### 2. Multi-call tool display shows commands twice (Task 8)

**Problem**: Tool calls are displayed live via `OnEvent(ToolCallEvent)`. Then in the result display, `printToolResult` with `multiCall=true` shows the command AGAIN:

```
🔧 bash                        ← live (ToolCallEvent)
   $ echo hello
🔧 bash                        ← live (ToolCallEvent)
   $ cat test.txt
✓ $ echo hello                 ← result (duplicates the call)
  (no output)
✓ $ cat test.txt               ← result (duplicates the call)
  hello
```

Commands appear twice. Confusing.

**Fix**: Drop the `multiCall` parameter. Always show results without command references — the commands were already displayed live:

```
🔧 bash
   $ echo hello
🔧 bash
   $ cat test.txt

✓ (no output)
✓ hello
```

For single call (the common case), the output is the same as before:
```
🔧 bash
   $ echo hello

✓ hello
```

Simplify `printToolResult`:

```go
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
```

In `runStep`, simplify the result display loop:

```go
for _, tr := range results {
    output := extractBashOutput(tr.ToolOutput())
    printToolResult(a.out, output, tr.IsError())
}
```

Remove `multiCall`, remove `command` parameter, remove `calls` pairing by index.

---

### 3. Goroutine leak in REPL signal handler (Task 10)

**Problem**: The signal handler goroutine runs `for range sigCh { ... }` which blocks forever after `RunREPL` returns. `signal.Stop(sigCh)` prevents new signals but doesn't close the channel. The goroutine stays alive.

**Fix**: Close the channel after stopping signal delivery:

```go
defer func() {
    signal.Stop(sigCh)
    close(sigCh) // terminates the goroutine's range loop
}()
```

This is safe because `signal.Stop` removes the channel from signal delivery before we close it — no writes after close.

---

### 4. `shouldRollback` is dead code (Task 8)

**Problem**: `shouldRollback` checks for `errMaxStepsReached`, but `errMaxStepsReached` is only returned from OUTSIDE the step loop (after it exhausts). Inside the loop, `shouldRollback` is called on errors from `runStep` — which can never be `errMaxStepsReached`. The check is always true for all inputs it actually receives.

**Fix**: Remove `shouldRollback`. Always rollback in the loop's error path — every error from `runStep` (that isn't `errContinue` or `nil`) indicates an invalid history state:

```go
if err := a.runStep(ctx, turnID, step, &stepsCompleted); err != nil {
    if errors.Is(err, errContinue) {
        continue
    }
    rollback()
    return err
}
```

The `errMaxStepsReached` path is already after the loop with no rollback. No change needed there.

---

### 5. `errContinue` sentinel is unusual (Task 8)

**Problem**: Using a sentinel error to mean "not an error, keep going" is a Go anti-pattern. Errors should mean something went wrong. This confuses readers and requires special handling (`errors.Is(err, errContinue)` check before the rollback path).

**Fix**: Return `(bool, error)` from `runStep`:

```go
// runStep returns (done, err):
//   done=false, err=nil → model called tools, continue to next step
//   done=true,  err=nil → turn completed successfully
//   done=_,     err!=nil → error, caller should rollback
func (a *Agent) runStep(ctx context.Context, turnID string, step int, stepsCompleted *int) (bool, error)
```

The caller becomes:

```go
for step := 1; step <= a.maxSteps; step++ {
    done, err := a.runStep(ctx, turnID, step, &stepsCompleted)
    if err != nil {
        rollback()
        return err
    }
    if done {
        if stepsCompleted > 1 {
            printTurnUsage(...)
        }
        return nil
    }
}
```

In `runStep`:
```go
case llm.StopReasonToolUse:
    return false, nil   // continue
case llm.StopReasonEndTurn:
    return true, nil    // done
case llm.StopReasonCancelled:
    return false, context.Canceled
```

---

### 6. `createProvider` return type (Task 11)

**Problem**: The plan has `createProvider` returning `*router.Provider`. The Agent constructor takes `llm.Provider` (an interface). While `*router.Provider` satisfies the interface, the function signature should return `llm.Provider` for consistency:

```go
func createProvider(ctx context.Context) (llm.Provider, error) {
```

This avoids importing `provider/router` in `main.go` for a type that's never used directly.

---

## ⚠️ Minor Issues

### 7. `aggregateTurn` duplicates tracker logic

The function replicates `Tracker.Aggregate()` line-for-line. Consider adding a comment `// TODO: upstream AggregateRecords([]Record) to usage package` so this gets cleaned up later.

### 8. `printStepHeader` box misaligns

The hardcoded padding `                              │` doesn't account for varying step/maxSteps widths:
- `Step 1/30` = 9 chars
- `Step 10/30` = 10 chars  
- `Step 1/5` = 8 chars

The closing `│` shifts. Fix: use `fmt.Sprintf` with width formatting or dynamically compute the padding.

### 9. Import management across tasks

Tasks 3→4 and 5→6 append code to existing files. The import blocks need to be merged at implementation time. The plan's code snippets show separate import blocks per task — the implementer must combine them into a single block per file.

### 10. Missing `ModelsProvider` / `ModelResolver` on blocking test provider

The `llm.Provider` interface requires `Named`, `ModelsProvider`, `ModelResolver`, and `Streamer`. The blocking provider in the fix for issue #1 needs stubs for all of these, not just `CreateStream`. Use `llm.NewProviderFromStreamFunc` or similar helper if available, or mock all methods.

---

## ✅ What's Good

| Aspect | Assessment |
|--------|-----------|
| Test coverage | Strong — every pure function has table-driven tests; RunTurn tested with fake provider; REPL tested with injected reader |
| Dependency graph | Clear, well-ordered, no circular deps |
| `io.Reader` injection for REPL | Clean testing without `os.Stdin` swap |
| `WithOutput(io.Writer)` option | Suppresses terminal noise in tests |
| `extractBashOutput` | Correctly parses JSON envelope for human-readable display |
| `CacheTTL1h` on system prompt | Important cost optimisation for REPL sessions |
| Rollback logic | Correctly prevents consecutive `role:"user"` messages |
| `RequestBuilder` passed directly to `CreateStream` | Simpler than calling `.Build()` separately |
| Two-tier truncation (20KB model / 300 char display) | Good separation |
| `formatUsageParts` shared helper | Eliminates duplication across step/turn/session |

---

## Summary

| # | Severity | Issue | Fix |
|---|----------|-------|-----|
| 1 | ❌ Critical | Flaky cancel test — race between ctx.Done() and buffered events | Use blocking provider (no events) |
| 2 | ❌ Medium | Multi-call display shows commands twice | Drop `multiCall`; always show results only |
| 3 | ❌ Medium | REPL goroutine leak | `close(sigCh)` after `signal.Stop` |
| 4 | ❌ Low | `shouldRollback` dead code | Remove; always rollback in loop error path |
| 5 | ❌ Low | `errContinue` anti-pattern | Return `(bool, error)` from `runStep` |
| 6 | ❌ Low | `createProvider` returns concrete type | Return `llm.Provider` interface |
| 7 | ⚠️ | `aggregateTurn` duplicates tracker | Add TODO comment |
| 8 | ⚠️ | Step header box misaligns | Dynamic padding |
| 9 | ⚠️ | Import merging across tasks | Note for implementer |
| 10 | ⚠️ | Blocking test provider needs full interface | Use helper or mock all methods |

**Recommendation**: Apply fixes 1–6, then the plan is ready to execute.
