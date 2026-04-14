# Plan: Reduce Cognitive Load in `infer.go` + Typed Flag Values

## Goal

1. Replace all stringly-typed flag fields in `inferOpts` with the actual `llm`
   types, backed by `encoding.TextMarshaler/Unmarshaler` so cobra/pflag can
   write directly into them via `f.TextVar`.
2. Eliminate the `inferOpts → inferSpec → Request` three-layer pipeline.
   Cobra writes into `inferOpts`; `runInfer` builds the `Request` inline.
3. Move `ToolChoice` parsing out of `infer.go` into the `llm` package.
4. Keep `runInfer`'s signature to `(ctx, inferOpts, root)`.

---

## Files to change

```
tool_choice.go                  ToolChoiceFlag type + ParseToolChoice
request_codec.go  (new, llm pkg)  TextMarshaler/Unmarshaler for ThinkingMode,
                                  Effort, OutputFormat
cmd/llmcli/cmds/infer.go       restructure NewInferCmd, inferOpts, runInfer;
                                delete inferSpec, buildInferSpec, parseToolChoice
```

`request.go` is NOT touched — types stay there, behaviour methods stay there.
The new `request_codec.go` adds the codec layer only.

---

## Step 1 — `tool_choice.go`

### 1a. `ParseToolChoice(s string) (ToolChoice, error)`

Package-level function; replaces `parseToolChoice` in `infer.go`.

```go
func ParseToolChoice(s string) (ToolChoice, error) {
    switch s {
    case "", "auto":
        return ToolChoiceAuto{}, nil
    case "none":
        return ToolChoiceNone{}, nil
    case "required":
        return ToolChoiceRequired{}, nil
    default:
        if name, ok := strings.CutPrefix(s, "tool:"); ok && name != "" {
            return ToolChoiceTool{Name: name}, nil
        }
        return nil, fmt.Errorf(
            "invalid tool-choice %q: must be auto, none, required, or tool:<name>", s)
    }
}
```

Add `"strings"` to `tool_choice.go` imports.

### 1b. `ToolChoiceFlag` — concrete TextMarshaler/Unmarshaler holder

The existing `ToolChoice` interface cannot implement these methods directly;
`ToolChoiceFlag` is a concrete wrapper used only at the flag layer.

```go
// ToolChoiceFlag is a pflag-compatible holder for a ToolChoice value.
// A zero-value ToolChoiceFlag (Value == nil) means "not specified by the caller";
// providers and runInfer apply their own defaults in that case.
type ToolChoiceFlag struct{ Value ToolChoice }

func (f ToolChoiceFlag) MarshalText() ([]byte, error) {
    if f.Value == nil {
        return []byte(""), nil
    }
    switch tc := f.Value.(type) {
    case ToolChoiceAuto:     return []byte("auto"), nil
    case ToolChoiceNone:     return []byte("none"), nil
    case ToolChoiceRequired: return []byte("required"), nil
    case ToolChoiceTool:     return []byte("tool:" + tc.Name), nil
    }
    return nil, fmt.Errorf("unknown ToolChoice type %T", f.Value)
}

func (f *ToolChoiceFlag) UnmarshalText(b []byte) error {
    if len(b) == 0 {
        f.Value = nil
        return nil
    }
    tc, err := ParseToolChoice(string(b))
    if err != nil {
        return err
    }
    f.Value = tc
    return nil
}
```

---

## Step 2 — `request_codec.go` (new file, `package llm`)

Add `encoding.TextMarshaler` and `encoding.TextUnmarshaler` for `ThinkingMode`,
`Effort`, and `OutputFormat`. Keep all type definitions and behaviour methods in
`request.go` — this file adds only the codec layer.

### Critical design note: `ThinkingMode`

`ThinkingAuto = ""` is the internal zero value, but the CLI shows and accepts
the string `"auto"`. Without explicit mapping, `--thinking=auto` produces
`ThinkingMode("auto")` — which is not `ThinkingAuto` and **fails `Validate()`**.
The codec bridges this gap:

```go
// MarshalText maps the zero value to the user-visible string "auto".
func (m ThinkingMode) MarshalText() ([]byte, error) {
    if m == ThinkingAuto { // ThinkingAuto = ""
        return []byte("auto"), nil
    }
    return []byte(m), nil
}

// UnmarshalText maps "auto" → ThinkingAuto (the zero value "").
func (m *ThinkingMode) UnmarshalText(b []byte) error {
    s := string(b)
    if s == "auto" {
        *m = ThinkingAuto
        return nil
    }
    v := ThinkingMode(s)
    if !v.Valid() {
        return fmt.Errorf("invalid thinking mode %q: must be auto, on, or off", s)
    }
    *m = v
    return nil
}
```

Side-effect: `f.TextVar(&opts.Thinking, "thinking", llm.ThinkingMode(""), ...)` will
show `(default auto)` in `--help`. This is correct and fixes the existing help-text
ambiguity.

### `Effort`

`EffortUnspecified = ""` — zero value means "provider decides". No alias needed.

```go
func (e Effort) MarshalText() ([]byte, error) { return []byte(e), nil }

func (e *Effort) UnmarshalText(b []byte) error {
    v := Effort(b)
    if !v.Valid() {
        return fmt.Errorf("invalid effort %q: must be low, medium, high, or max", v)
    }
    *e = v
    return nil
}
```

`Effort.Valid()` already accepts `EffortUnspecified` (`""`), so an unset flag (which
pflag leaves at the zero value without calling `UnmarshalText`) is always valid.

### `OutputFormat`

```go
func (f OutputFormat) MarshalText() ([]byte, error) { return []byte(f), nil }

func (f *OutputFormat) UnmarshalText(b []byte) error {
    v := OutputFormat(b)
    switch v {
    case "", OutputFormatText, OutputFormatJSON:
        *f = v
        return nil
    }
    return fmt.Errorf("invalid output-format %q: must be text or json", v)
}
```

### Imports for `text_values.go`

```go
import (
    "fmt"
    "encoding" // for doc linkage — not strictly needed; MarshalText/UnmarshalText
               // satisfy the interfaces implicitly
)
```

Actually no import beyond `"fmt"` is needed — the interface satisfaction is
implicit in Go.

---

## Step 3 — `inferOpts` restructured

Remove all `string`-typed flag fields. Add `DemoToolHandlers` to carry the tool
handlers through to the proc chain (replaces `inferSpec.ToolHandlers`).

```go
type inferOpts struct {
    // Populated from positional arg in RunE, not a flag.
    UserMsg string

    // Flags — cobra writes directly via the appropriate Var methods.
    Model        string
    System       string
    Verbose      bool
    DemoTools    bool
    MaxTokens    int
    Temperature  float64
    TopP         float64
    TopK         int
    Thinking     llm.ThinkingMode   // f.TextVar — pflag calls UnmarshalText on set
    Effort       llm.Effort         // f.TextVar
    ToolChoice   llm.ToolChoiceFlag // f.TextVar; zero Value = "not specified"
    OutputFormat llm.OutputFormat   // f.TextVar

    // Populated by runInfer, not from flags.
    // Holds the demo-tool handlers when DemoTools is true.
    demoToolHandlers []tool.NamedHandler
}
```

`demoToolHandlers` is unexported because it is never set by cobra — only by
`runInfer` after it calls `buildDemoTools()`.

---

## Step 4 — `NewInferCmd` restructured

No top-level `var` block. No copy-construct struct literal in `RunE`.

```go
func NewInferCmd(root *RootFlags) *cobra.Command {
    var opts inferOpts

    cmd := &cobra.Command{
        Use:   "infer <message>",
        Short: "Send a message to an LLM and stream the response",
        Long:  `...`,
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            opts.UserMsg = args[0]
            return runInfer(cmd.Context(), opts, root)
        },
    }

    f := cmd.Flags()
    f.StringVarP(&opts.Model,       "model",         "m", "fast", "Model alias or full path")
    f.StringVarP(&opts.System,      "system",        "s", "",     "System prompt")
    f.BoolVarP(&opts.Verbose,       "verbose",       "v", false,  "Verbose output")
    f.BoolVar(&opts.DemoTools,      "demo-tools",        false,   "Enable demo tool loop")
    f.IntVar(&opts.MaxTokens,       "max-tokens",        8_000,   "Max tokens (0 = provider default)")
    f.Float64Var(&opts.Temperature, "temperature",        0,      "Sampling temperature 0.0–2.0 (0 = provider default)")
    f.Float64Var(&opts.TopP,        "top-p",              0,      "Nucleus sampling 0.0–1.0 (0 = provider default)")
    f.IntVar(&opts.TopK,            "top-k",              0,      "Top-K limit (0 = provider default)")
    f.TextVar(&opts.Thinking,     "thinking",      llm.ThinkingMode(""),   "Thinking mode: auto, on, off")
    f.TextVar(&opts.Effort,       "effort",        llm.Effort(""),         "Effort: low, medium, high, max")
    f.TextVar(&opts.ToolChoice,   "tool-choice",   llm.ToolChoiceFlag{},   "Tool selection: auto, none, required, tool:<name>")
    f.TextVar(&opts.OutputFormat, "output-format", llm.OutputFormat(""),   "Output format: text, json")

    return cmd
}
```

**pflag pointer mechanics**: `f.TextVar` takes `p encoding.TextUnmarshaler`.
Because `UnmarshalText` is on the pointer receiver for all four types, passing
`&opts.Thinking` (type `*llm.ThinkingMode`) satisfies `encoding.TextUnmarshaler`.
Passing `opts.Thinking` directly would not compile — the compiler enforces this. ✅

**Default value display**: pflag calls `MarshalText()` on the default value to
produce the string shown in `--help`. With the codec above:
- `ThinkingMode("").MarshalText()` → `"auto"` → help shows `(default auto)` ✅
- `Effort("").MarshalText()` → `""` → help shows `(default "")` — acceptable;
  usage string explains it
- `ToolChoiceFlag{}.MarshalText()` → `""` → help shows `(default "")`
- `OutputFormat("").MarshalText()` → `""` → help shows `(default "")`

---

## Step 5 — Delete `inferSpec`, `buildInferSpec`, `parseToolChoice`

Replace with:

### 5a. `inferOpts.buildMessages() llm.Messages`

```go
func (o inferOpts) buildMessages() llm.Messages {
    cacheHint := &llm.CacheHint{Enabled: true, TTL: "1h"}

    system := o.System
    if system == "" && o.DemoTools {
        system = defaultDemoSystemPrompt
    }

    msgs := make(llm.Messages, 0, 2)
    if system != "" {
        m := msg.System(system).Build()
        m.CacheHint = cacheHint
        msgs = append(msgs, m)
    }
    m := msg.User(o.UserMsg).Build()
    m.CacheHint = cacheHint
    return append(msgs, m)
}
```

### 5b. `buildDemoTools() ([]tool.Definition, []tool.NamedHandler)`

Package-level function; extracted from `buildInferSpec`. Returns both the tool
definitions (for the request) and the handlers (for the proc chain).

```go
func buildDemoTools() ([]tool.Definition, []tool.NamedHandler) {
    defs := tool.NewToolSet(
        tool.NewSpec[addFactParams]("add_fact", "Store a single fact"),
        tool.NewSpec[completeTurnParams]("complete_turn", "Complete the current turn"),
    ).Definitions()

    handlers := []tool.NamedHandler{
        tool.NewHandler("complete_turn", func(_ context.Context, in completeTurnParams) (*defaultToolResult, error) {
            return &defaultToolResult{Message: "Turn complete", Success: in.Success}, nil
        }),
        tool.NewHandler("add_fact", func(_ context.Context, in addFactParams) (*defaultToolResult, error) {
            return &defaultToolResult{Message: fmt.Sprintf("Fact added: %s", in.Fact), Success: true}, nil
        }),
    }
    return defs, handlers
}
```

### 5c. Tool-choice resolution (3 lines inline in `runInfer`)

```go
toolChoice := opts.ToolChoice.Value // nil = not specified by --tool-choice flag
if opts.DemoTools && toolChoice == nil {
    toolChoice = llm.ToolChoiceRequired{} // demo-tools default; overridable by flag
}
```

---

## Step 6 — `runInfer` final shape

```go
func runInfer(ctx context.Context, opts inferOpts, root *RootFlags) error {
    // 1. Provider
    httpClient, logHandler := root.BuildHTTPClient()
    provider, err := createProvider(ctx, httpClient, root.BuildLLMOptions(logHandler)...)
    if err != nil {
        return err
    }

    // 2. Messages
    msgs := opts.buildMessages()

    // 3. Tool definitions + handlers (demo-tools only)
    var tools []tool.Definition
    toolChoice := opts.ToolChoice.Value
    if opts.DemoTools {
        if toolChoice == nil {
            toolChoice = llm.ToolChoiceRequired{}
        }
        tools, opts.demoToolHandlers = buildDemoTools()
    }

    // 4. Request
    req := llm.Request{
        Model:        opts.Model,
        Messages:     msgs,
        Effort:       opts.Effort,
        Thinking:     opts.Thinking,
        ToolChoice:   toolChoice,
        Tools:        tools,
        MaxTokens:    opts.MaxTokens,
        Temperature:  opts.Temperature,
        TopP:         opts.TopP,
        TopK:         opts.TopK,
        OutputFormat: opts.OutputFormat,
    }

    // 5. Token estimate (verbose) — uses msgs and tools directly, no spec needed
    if opts.Verbose { ... }

    // 6. Stream
    stream, err := provider.CreateStream(ctx, req)
    if err != nil {
        return fmt.Errorf("create stream: %w", err)
    }

    // 7. Event processing — replace spec.ToolHandlers with opts.demoToolHandlers
    ...
    if len(opts.demoToolHandlers) > 0 {
        proc = proc.HandleTool(opts.demoToolHandlers...)
    }

    // 8. Post-stream output (unchanged)
    ...
}
```

---

## What does NOT change

- The stream processing block (event handlers, verbose sections, tool loop body)
- All `print*` helpers
- `provider` packages — no changes
- `llm.Request`, `llm.Messages` — no changes to types
- Token counting — uses `msgs` and `tools` directly, same as now

---

## Tests to add / update

### `request_codec.go` → `request_codec_test.go`

Table-driven round-trip tests for each type. Cover:

- **`ThinkingMode`**: `"" → "auto"` (MarshalText), `"auto" → ""` (UnmarshalText),
  `"on"` and `"off"` round-trip, invalid input returns error
- **`Effort`**: each valid value round-trips, invalid input returns error
- **`OutputFormat`**: `"text"`, `"json"`, `""` round-trip, invalid returns error

### `tool_choice.go` → `tool_choice_test.go` (or inline)

- **`ParseToolChoice`**: `"auto"`, `""`, `"none"`, `"required"`, `"tool:my_tool"`,
  invalid → error
- **`ToolChoiceFlag.MarshalText/UnmarshalText`**: round-trip for all four modes,
  zero value, invalid input

### `infer.go` — update existing tests

- Any test that constructs `inferOpts` with string fields needs updating to typed
  fields
- `TestParseToolChoice` (if it exists) moves to `tool_choice_test.go`
