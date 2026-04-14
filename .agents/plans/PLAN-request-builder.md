# Plan: Request Builder — Message/Tool Methods + infer Migration

**Design**: `.agents/plans/DESIGN-request-builder.md`
**Estimated total time**: ~25 minutes

---

## Context

`request_builder.go` exists but covers only primitive fields (model, effort,
thinking, sampling params). It has two bugs and cannot build messages or
register tools. `cmd/llmcli/cmds/infer.go` assembles `llm.Request` by hand.
This plan extends the builder and migrates `infer.go` to use it end-to-end.

**Baseline**: clean working tree after `7ed80e1`
(feat(llmcli): add sampling flags and typed flag infrastructure)

---

## Task 1 — `message.go`: re-export CacheOpt, CacheTTL

**File**: `message.go`
**Estimated time**: 2 minutes

Add type aliases and constants so callers using `RequestBuilder` need not
import `msg` directly.

**Code to write** — add inside the `type (...)` block after `CacheHint`:

```go
	// CacheOpt and CacheTTL are re-exported from the msg package so callers
	// using RequestBuilder do not need to import msg directly.
	CacheOpt = msg.CacheOpt
	CacheTTL = msg.CacheTTL
```

**Code to write** — add inside the `const (...)` block after `RoleDeveloper`:

```go

	// Cache TTL convenience aliases.
	CacheTTL5m = msg.CacheTTL5m
	CacheTTL1h = msg.CacheTTL1h
```

**Resulting file** (22 → 30 lines):

```go
package llm

import "github.com/codewandler/llm/msg"

type (
	Role      = msg.Role
	Message   = msg.Message
	Messages  = msg.Messages
	CacheHint = msg.CacheHint

	// CacheOpt and CacheTTL are re-exported from the msg package so callers
	// using RequestBuilder do not need to import msg directly.
	CacheOpt = msg.CacheOpt
	CacheTTL = msg.CacheTTL
)

const (
	RoleSystem    = msg.RoleSystem
	RoleUser      = msg.RoleUser
	RoleAssistant = msg.RoleAssistant
	RoleTool      = msg.RoleTool
	RoleDeveloper = msg.RoleDeveloper

	// Cache TTL convenience aliases.
	CacheTTL5m = msg.CacheTTL5m
	CacheTTL1h = msg.CacheTTL1h
)

func System(text string) Message    { return msg.System(text).Build() }
func User(text string) Message      { return msg.User(text).Build() }
func Assistant(text string) Message { return msg.Assistant(msg.Text(text)).Build() }
```

**Verification**:
```bash
go build ./...
```

---

## Task 2 — `request_builder.go`: fix bugs + add message/tool methods

**File**: `request_builder.go`
**Estimated time**: 8 minutes

Three things to fix/add:

1. **Constructor bug** (line 88): `NewRequestBuilder()` calls `newDefaultRequest()`
   which pre-fills `Temperature: 0.7`, `Effort: EffortLow`, `Thinking: ThinkingOff`,
   `ToolChoice: ToolChoiceAuto{}`, and `CacheHint: msg.NewCacheHint()`. These
   silently override provider defaults. Change to zero-value.
2. **`BuildRequest` bug** (line 91): `NewRequestBuilder().Build()` silently drops
   the `opts` argument. Fix to pass `opts...` through.
3. **Missing methods**: `System`, `User`, `Append`, `Tools`, `ToolChoice`, `Apply`.
4. **Missing `With*` constructors**: one per field/concept for the options-based
   style.

**Implementation note — `Cache()` with no args is not a no-op**:
`msg.Builder.Cache(opts ...CacheOpt)` always calls `msg.NewCacheHint(opts...)`,
which returns `&CacheHint{Enabled: true, TTL: "5m"}` even when `opts` is empty.
Therefore `System` and `User` must guard with `if len(cache) > 0` before calling
`.Cache()`, so that passing no cache opts leaves `Message.CacheHint == nil`.

**Code to write** — replace the entire file:

```go
package llm

import (
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

type RequestOption func(r *Request)

type RequestBuilder struct {
	req *Request
}

// Apply applies functional options to the builder and returns b for chaining.
// Build(opts...) internally delegates to Apply, so both are interchangeable
// for the terminal options; Apply is preferred when options are pre-assembled.
func (b *RequestBuilder) Apply(opts ...RequestOption) *RequestBuilder {
	for _, opt := range opts {
		opt(b.req)
	}
	return b
}

func (b *RequestBuilder) Build(opts ...RequestOption) (Request, error) {
	b.Apply(opts...)

	// TODO: b.req.normalize()

	if err := b.req.Validate(); err != nil {
		return Request{}, err
	}

	return *b.req, nil
}

// --- Fluent setter methods ---

func (b *RequestBuilder) Model(modelID string) *RequestBuilder {
	b.req.Model = modelID
	return b
}

func (b *RequestBuilder) Thinking(mode ThinkingMode) *RequestBuilder {
	b.req.Thinking = mode
	return b
}

func (b *RequestBuilder) Effort(level Effort) *RequestBuilder {
	b.req.Effort = level
	return b
}

func (b *RequestBuilder) MaxTokens(maxTokens int) *RequestBuilder {
	b.req.MaxTokens = maxTokens
	return b
}

func (b *RequestBuilder) Temperature(temperature float64) *RequestBuilder {
	b.req.Temperature = temperature
	return b
}

// OutputFormat sets the output format of the response.
func (b *RequestBuilder) OutputFormat(format OutputFormat) *RequestBuilder {
	b.req.OutputFormat = format
	return b
}

// TopK sets the top-k parameter for sampling.
func (b *RequestBuilder) TopK(k int) *RequestBuilder {
	b.req.TopK = k
	return b
}

func (b *RequestBuilder) TopP(p float64) *RequestBuilder {
	b.req.TopP = p
	return b
}

func (b *RequestBuilder) Coding() *RequestBuilder {
	return b.Thinking(ThinkingOn).
		Effort(EffortHigh).
		Temperature(0.1).
		MaxTokens(16_000)
}

// System appends a system message. Pass CacheTTL1h or CacheTTL5m to enable
// prompt caching for this message. Omitting cache leaves CacheHint nil.
func (b *RequestBuilder) System(text string, cache ...CacheOpt) *RequestBuilder {
	mb := msg.System(text)
	if len(cache) > 0 {
		mb = mb.Cache(cache...)
	}
	b.req.Messages = append(b.req.Messages, mb.Build())
	return b
}

// User appends a user message. Pass CacheTTL1h or CacheTTL5m to enable
// prompt caching for this message. Omitting cache leaves CacheHint nil.
func (b *RequestBuilder) User(text string, cache ...CacheOpt) *RequestBuilder {
	mb := msg.User(text)
	if len(cache) > 0 {
		mb = mb.Cache(cache...)
	}
	b.req.Messages = append(b.req.Messages, mb.Build())
	return b
}

// Append appends pre-built messages (assistant turns, tool results, etc.).
func (b *RequestBuilder) Append(msgs ...Message) *RequestBuilder {
	b.req.Messages = append(b.req.Messages, msgs...)
	return b
}

// Tools sets the tool definitions available to the model.
func (b *RequestBuilder) Tools(defs ...tool.Definition) *RequestBuilder {
	b.req.Tools = defs
	return b
}

// ToolChoice sets the tool selection strategy.
func (b *RequestBuilder) ToolChoice(tc ToolChoice) *RequestBuilder {
	b.req.ToolChoice = tc
	return b
}

// --- Functional option constructors (With* prefix) ---
//
// Each With* function returns a RequestOption that sets a single field.
// They compose with Apply and BuildRequest, and can be accumulated in
// []RequestOption slices for programmatic configuration.

func WithModel(model string) RequestOption {
	return func(r *Request) { r.Model = model }
}

func WithThinking(mode ThinkingMode) RequestOption {
	return func(r *Request) { r.Thinking = mode }
}

func WithEffort(level Effort) RequestOption {
	return func(r *Request) { r.Effort = level }
}

func WithMaxTokens(n int) RequestOption {
	return func(r *Request) { r.MaxTokens = n }
}

func WithTemperature(t float64) RequestOption {
	return func(r *Request) { r.Temperature = t }
}

func WithOutputFormat(f OutputFormat) RequestOption {
	return func(r *Request) { r.OutputFormat = f }
}

func WithTopK(k int) RequestOption {
	return func(r *Request) { r.TopK = k }
}

func WithTopP(p float64) RequestOption {
	return func(r *Request) { r.TopP = p }
}

// WithSystem appends a system message. Same cache nil-guard semantics as
// the fluent System method: omitting cache leaves CacheHint nil.
func WithSystem(text string, cache ...CacheOpt) RequestOption {
	return func(r *Request) {
		mb := msg.System(text)
		if len(cache) > 0 {
			mb = mb.Cache(cache...)
		}
		r.Messages = append(r.Messages, mb.Build())
	}
}

// WithUser appends a user message. Same cache nil-guard semantics as
// the fluent User method: omitting cache leaves CacheHint nil.
func WithUser(text string, cache ...CacheOpt) RequestOption {
	return func(r *Request) {
		mb := msg.User(text)
		if len(cache) > 0 {
			mb = mb.Cache(cache...)
		}
		r.Messages = append(r.Messages, mb.Build())
	}
}

// WithMessages appends pre-built messages (assistant turns, tool results, etc.).
func WithMessages(msgs ...Message) RequestOption {
	return func(r *Request) { r.Messages = append(r.Messages, msgs...) }
}

// WithTools sets the tool definitions available to the model.
func WithTools(defs ...tool.Definition) RequestOption {
	return func(r *Request) { r.Tools = defs }
}

// WithToolChoice sets the tool selection strategy.
func WithToolChoice(tc ToolChoice) RequestOption {
	return func(r *Request) { r.ToolChoice = tc }
}

// --- Constructors ---

// NewRequestBuilder returns a zero-value builder. All fields default to their
// provider-level defaults (zero values). Call Build() only after setting Model.
func NewRequestBuilder() *RequestBuilder { return &RequestBuilder{req: &Request{}} }

// BuildRequest is a convenience wrapper; opts are passed through to Build.
func BuildRequest(opts ...RequestOption) (Request, error) {
	return NewRequestBuilder().Build(opts...)
}
```

**Estimated time**: 8 minutes

**Verification**:
```bash
go build ./...
go test . -run TestRequestBuilder -v
go test . -run TestBuildRequest -v
go test . -run TestWith -v
```

---

## Task 3 — `request_builder_test.go` (new file): tests for new methods

**File**: `request_builder_test.go` (create new, `package llm`)
**Estimated time**: 5 minutes

No existing test file covers `RequestBuilder` at the `llm` package level.
Tests cover: zero-value constructor, fluent methods, `Apply`, `With*`
constructors (representative sample + all non-trivial cache-guard paths),
and the `BuildRequest` regression.

**Code to write**:

```go
package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/tool"
)

// --- Constructor ---

func TestRequestBuilder_ZeroValueConstructor(t *testing.T) {
	b := NewRequestBuilder()

	// Verify no opinionated defaults are pre-filled.
	assert.Equal(t, ThinkingAuto, b.req.Thinking)
	assert.Equal(t, EffortUnspecified, b.req.Effort)
	assert.Equal(t, float64(0), b.req.Temperature)
	assert.Nil(t, b.req.ToolChoice)
	assert.Nil(t, b.req.CacheHint)
}

// --- Fluent interface ---

func TestRequestBuilder_System_User_Roles(t *testing.T) {
	req, err := NewRequestBuilder().
		Model("test-model").
		System("You are helpful.").
		User("Hello").
		Build()

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleSystem, req.Messages[0].Role)
	assert.Equal(t, RoleUser, req.Messages[1].Role)
}

func TestRequestBuilder_System_WithCache(t *testing.T) {
	req, err := NewRequestBuilder().
		Model("test-model").
		System("You are helpful.", CacheTTL1h).
		User("Hello", CacheTTL1h).
		Build()

	require.NoError(t, err)
	require.NotNil(t, req.Messages[0].CacheHint)
	assert.Equal(t, "1h", req.Messages[0].CacheHint.TTL)
	require.NotNil(t, req.Messages[1].CacheHint)
	assert.Equal(t, "1h", req.Messages[1].CacheHint.TTL)
}

func TestRequestBuilder_System_NoCache(t *testing.T) {
	// Passing no cache opts must leave CacheHint nil.
	// Cache() with no args still calls NewCacheHint() and produces a non-nil
	// hint — the builder guards with len(cache) > 0 to preserve nil semantics.
	req, err := NewRequestBuilder().
		Model("test-model").
		System("You are helpful.").
		User("Hello").
		Build()

	require.NoError(t, err)
	assert.Nil(t, req.Messages[0].CacheHint)
	assert.Nil(t, req.Messages[1].CacheHint)
}

func TestRequestBuilder_Append(t *testing.T) {
	assistant := Assistant("Sure, here you go.")

	req, err := NewRequestBuilder().
		Model("test-model").
		User("Hello").
		Append(assistant).
		Build()

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleAssistant, req.Messages[1].Role)
}

func TestRequestBuilder_Tools_ToolChoice(t *testing.T) {
	spec := tool.NewSpec[struct{}]("ping", "Ping the system")

	req, err := NewRequestBuilder().
		Model("test-model").
		User("Ping").
		Tools(spec.Definition()).
		ToolChoice(ToolChoiceRequired{}).
		Build()

	require.NoError(t, err)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "ping", req.Tools[0].Name)
	assert.Equal(t, ToolChoiceRequired{}, req.ToolChoice)
}

// --- Apply ---

func TestRequestBuilder_Apply_FluentMix(t *testing.T) {
	// Apply a pre-assembled slice, then add a message fluently.
	baseOpts := []RequestOption{
		WithModel("test-model"),
		WithMaxTokens(512),
	}

	req, err := NewRequestBuilder().
		Apply(baseOpts...).
		User("Hello").
		Build()

	require.NoError(t, err)
	assert.Equal(t, "test-model", req.Model)
	assert.Equal(t, 512, req.MaxTokens)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, RoleUser, req.Messages[0].Role)
}

func TestRequestBuilder_Apply_Chainable(t *testing.T) {
	// Apply must return the builder for further chaining.
	req, err := NewRequestBuilder().
		Apply(WithModel("test-model")).
		Apply(WithUser("Hello")).
		Build()

	require.NoError(t, err)
	assert.Equal(t, "test-model", req.Model)
}

// --- With* option constructors ---

func TestBuildRequest_FullyOptionBased(t *testing.T) {
	// Equivalent to the fluent TestRequestBuilder_System_User_Roles.
	req, err := BuildRequest(
		WithModel("test-model"),
		WithSystem("You are helpful."),
		WithUser("Hello"),
	)

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleSystem, req.Messages[0].Role)
	assert.Equal(t, RoleUser, req.Messages[1].Role)
}

func TestWithSystem_Cache(t *testing.T) {
	req, err := BuildRequest(
		WithModel("test-model"),
		WithSystem("prompt", CacheTTL1h),
		WithUser("hi"),
	)

	require.NoError(t, err)
	require.NotNil(t, req.Messages[0].CacheHint)
	assert.Equal(t, "1h", req.Messages[0].CacheHint.TTL)
	assert.Nil(t, req.Messages[1].CacheHint) // WithUser without cache
}

func TestWithSystem_NoCache(t *testing.T) {
	// Same nil-guard as the fluent method.
	req, err := BuildRequest(
		WithModel("test-model"),
		WithSystem("prompt"),
		WithUser("hi"),
	)

	require.NoError(t, err)
	assert.Nil(t, req.Messages[0].CacheHint)
}

func TestWithMessages_Append(t *testing.T) {
	assistant := Assistant("Here you go.")

	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("Hello"),
		WithMessages(assistant),
	)

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleAssistant, req.Messages[1].Role)
}

func TestWithTools_WithToolChoice(t *testing.T) {
	spec := tool.NewSpec[struct{}]("search", "Search")

	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("Find it"),
		WithTools(spec.Definition()),
		WithToolChoice(ToolChoiceAuto{}),
	)

	require.NoError(t, err)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "search", req.Tools[0].Name)
	assert.Equal(t, ToolChoiceAuto{}, req.ToolChoice)
}

func TestBuildRequest_PassesOptsThrough(t *testing.T) {
	// Regression: BuildRequest previously silently ignored its opts argument.
	req, err := BuildRequest(func(r *Request) {
		r.Model = "injected-model"
		r.Messages = Messages{User("hi")}
	})

	require.NoError(t, err)
	assert.Equal(t, "injected-model", req.Model)
}
```

**Verification**:
```bash
go test . -run TestRequestBuilder -v
go test . -run TestBuildRequest -v
```

---

## Task 4 — `cmd/llmcli/cmds/infer.go`: migrate runInfer to builder

**File**: `cmd/llmcli/cmds/infer.go`
**Estimated time**: 4 minutes

Four sub-steps. Apply them in order.

---

### 4a. Delete `buildMessages()` — lines 86–103

Current block (exact):

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

Delete entirely. `resolveToolChoice()` (lines 105–116) is kept unchanged.

---

### 4b. Replace `msg.RoleTool` with `llm.RoleTool` — line 343

**Important**: `msg` is still referenced at line 343 after removing `buildMessages()`.
Change it to use the re-exported constant so the `msg` import becomes unused:

Current (line 343):
```go
		if m.Role == msg.RoleTool {
```

Replace with:
```go
		if m.Role == llm.RoleTool {
```

Then remove `"github.com/codewandler/llm/msg"` from the import block (line 11).

---

### 4c. Replace the message-building + request-literal block — lines 126–148

Current block:

```go
	// Messages
	msgs := opts.buildMessages()

	// Tool definitions + handlers (demo-tools only)
	var tools []tool.Definition
	toolChoice := opts.resolveToolChoice()
	if opts.DemoTools {
		tools, opts.demoToolHandlers = buildDemoTools()
	}

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
```

Replace with:

```go
	// System prompt: explicit --system takes precedence; demo-tools fills the gap.
	system := opts.System
	if system == "" && opts.DemoTools {
		system = defaultDemoSystemPrompt
	}

	// Build request.
	b := llm.NewRequestBuilder().
		Model(opts.Model).
		Effort(opts.Effort).
		Thinking(opts.Thinking).
		MaxTokens(opts.MaxTokens).
		Temperature(opts.Temperature).
		TopP(opts.TopP).
		TopK(opts.TopK).
		OutputFormat(opts.OutputFormat)
	if system != "" {
		b = b.System(system, llm.CacheTTL1h)
	}
	b = b.User(opts.UserMsg, llm.CacheTTL1h)

	toolChoice := opts.resolveToolChoice()
	if opts.DemoTools {
		defs, handlers := buildDemoTools()
		opts.demoToolHandlers = handlers
		b = b.Tools(defs...).ToolChoice(toolChoice)
	} else if toolChoice != nil {
		b = b.ToolChoice(toolChoice)
	}

	req, err := b.Build()
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
```

---

### 4d. Update the token-estimate block — lines 156–160

The local variables `msgs` and `tools` no longer exist after 4c.
Read them from `req` instead.

Current:
```go
			est, err := tc.CountTokens(ctx, tokencount.TokenCountRequest{
				Model:    opts.Model,
				Messages: msgs,
				Tools:    tools,
			})
```

Replace with:
```go
			est, err := tc.CountTokens(ctx, tokencount.TokenCountRequest{
				Model:    req.Model,
				Messages: req.Messages,
				Tools:    req.Tools,
			})
```

**Verification**:
```bash
go build ./cmd/llmcli/...
go test ./cmd/llmcli/cmds/... -v
```

---

## Task 5 — `cmd/llmcli/cmds/infer_test.go`: remove buildMessages tests

**File**: `cmd/llmcli/cmds/infer_test.go`
**Estimated time**: 1 minute

Delete the three `TestInferOpts_BuildMessages_*` tests — `buildMessages()` no
longer exists. Remove the `"github.com/codewandler/llm/msg"` import that was
only needed by those tests.

**Delete**:
- `TestInferOpts_BuildMessages_NoDemoTools`
- `TestInferOpts_BuildMessages_WithSystem`
- `TestInferOpts_BuildMessages_DemoToolsDefaultSystem`
- `"github.com/codewandler/llm/msg"` import

**Keep unchanged**:
- `TestBuildDemoTools`
- `TestInferOpts_ResolveToolChoice`

**Resulting file**:

```go
package cmds

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestBuildDemoTools(t *testing.T) {
	defs, handlers := buildDemoTools()

	require.Len(t, defs, 2)
	assert.Equal(t, "add_fact", defs[0].Name)
	assert.Equal(t, "complete_turn", defs[1].Name)

	require.Len(t, handlers, 2)
}

func TestInferOpts_ResolveToolChoice(t *testing.T) {
	tests := []struct {
		name string
		opts inferOpts
		want llm.ToolChoice
	}{
		{
			name: "no flags → nil",
			opts: inferOpts{},
			want: nil,
		},
		{
			name: "demo-tools only → ToolChoiceRequired",
			opts: inferOpts{DemoTools: true},
			want: llm.ToolChoiceRequired{},
		},
		{
			name: "demo-tools + explicit auto → ToolChoiceAuto (flag wins)",
			opts: inferOpts{
				DemoTools:  true,
				ToolChoice: llm.ToolChoiceFlag{Value: llm.ToolChoiceAuto{}},
			},
			want: llm.ToolChoiceAuto{},
		},
		{
			name: "demo-tools + explicit none → ToolChoiceNone (flag wins)",
			opts: inferOpts{
				DemoTools:  true,
				ToolChoice: llm.ToolChoiceFlag{Value: llm.ToolChoiceNone{}},
			},
			want: llm.ToolChoiceNone{},
		},
		{
			name: "explicit required without demo-tools → ToolChoiceRequired",
			opts: inferOpts{
				ToolChoice: llm.ToolChoiceFlag{Value: llm.ToolChoiceRequired{}},
			},
			want: llm.ToolChoiceRequired{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.opts.resolveToolChoice())
		})
	}
}
```

**Verification**:
```bash
go test ./cmd/llmcli/cmds/... -v
```

---

## Final Verification

```bash
go build ./...
go vet ./...
go test $(go list ./... | grep -v integration) -count=1
```

Expected: all packages pass, no compilation errors, no vet warnings.

---

## What Does NOT Change

- `resolveToolChoice()` on `inferOpts` — kept, used in Task 4c
- `buildDemoTools()` — kept unchanged
- All provider packages — not touched
- `llm.Request` type — not touched
- `request_codec.go` / `tool_choice.go` — not touched
- The stream processing block, print helpers, verbose sections — not touched
