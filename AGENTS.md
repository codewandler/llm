# AGENTS.md - Developer Guide for Coding Agents

This guide is for AI coding agents working in this repository. Follow these conventions to maintain consistency.

## Communication Guidelines

- Keep end-of-turn communication concise. Prefer a brief direct note over a long recap unless the user asks for detail.

---

## Build, Test, and Lint Commands

### Building
```bash
# Build all packages
go build ./...

# Check for compilation errors without building binaries
go build -o /dev/null ./...
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test function
go test -v -run TestFunctionName ./provider/anthropic

# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Prefer narrow package runs while developing
go test ./provider/anthropic/...
go test ./provider/openrouter/...
go test ./integration/...
```

### Linting and Formatting
```bash
go fmt ./...          # format (always do before committing)
go mod tidy           # tidy dependencies
go vet ./...          # vet for suspicious constructs
golangci-lint run     # if available
task install          # install llmcli binary
```


### Integration matrix testing

The main integration suite is now **service-first**.

- Run with: `RUN_INTEGRATION=1 go test -tags integration ./integration -run TestIntegrationMatrix -v`
- The matrix executes scenarios through `llm.Service` / `auto.New(...)`, not by constructing provider packages directly.
- Matrix targets declare:
  - model selector
  - expected resolved service/provider/API type
  - capability flags (reasoning / effort / thinking toggle)
- The runner calls `Service.ExplainModel(...)` before `CreateStream(...)` and asserts routing expectations as well as output behavior.

You can emit artifacts for review:

```bash
RUN_INTEGRATION=1 MATRIX_RESULTS_JSON=docs/integration-matrix.json MATRIX_RESULTS_MD=docs/integration-matrix.md go test -tags integration ./integration -run TestIntegrationMatrix -v
```

Notes:
- `docs/integration-matrix.md` is the human-readable snapshot
- `docs/integration-matrix.json` is the machine-readable snapshot
- Use these artifacts to track provider health across Anthropic, Claude, OpenAI, OpenRouter, Codex, and MiniMax

### Quick Testing with llmcli

```bash
go run ./cmd/llmcli auth status                          # Check Claude OAuth credentials
go run ./cmd/llmcli infer "Hello"                        # Quick inference test
go run ./cmd/llmcli infer -v -m default "Explain Go channels"  # Verbose: tokens, cost, timing
```

---

## Project Architecture

```
llm/
├── api/                # Wire-protocol packages: apicore, completions, messages, responses, unified
├── catalog/            # Built-in model catalog, sources, merge/query/view
├── cmd/llmcli/         # CLI for inference, model inspection, auth helpers
├── internal/modelcatalog/ # Built-in catalog loading and canonicalization
├── internal/modelview/ # Catalog projections and visible-model views
├── internal/providerregistry/ # Provider detect/build registry
├── auto/               # Convenience service builder
├── internal/providercore/ # Shared provider client/config plumbing
├── llm.go              # Top-level package marker
├── event.go            # Envelope, EventType, event structs
├── event_delta.go      # DeltaEvent and delta payload types
├── event_publisher.go  # NewEventPublisher and envelope publishing
├── event_processor.go  # NewEventProcessor and Result implementation
├── request.go          # Request, ThinkingMode, Effort, ApiType, validation
├── request_builder.go  # Buildable, RequestBuilder, fluent request construction
├── message.go          # Re-exports from msg package
├── msg/                # Canonical message model and builders
├── errors.go           # ProviderError and sentinel errors
├── model.go            # Model, Models, resolver helpers
├── service.go         # llm.Service, llm.New(...), service options
├── tokencount/         # Token estimation helpers and interfaces
├── usage/              # Pricing, records, drift, budgets, tracking
├── tool/               # Tool definitions and typed tool handling
├── llmtest/            # Test helpers for stream consumers
│
└── provider/
    ├── anthropic/      # Direct Anthropic Messages API
    │   └── claude/     # OAuth-based Claude provider
    ├── bedrock/        # AWS Bedrock
    ├── codex/          # ChatGPT/Codex auth-backed provider
    ├── dockermr/       # Docker Model Runner
    ├── fake/           # Test provider
    ├── minimax/        # MiniMax Anthropic-compatible provider
    ├── ollama/         # Local Ollama
    ├── openai/         # OpenAI provider
    ├── openrouter/     # Multi-wire OpenRouter provider
```

**Key concepts:**
- Real backend providers implement `llm.Provider`
- `llm.Service` is now the preferred runtime orchestration object
- `CreateStream(ctx, src)` accepts any `llm.Buildable` (`llm.Request` or `*llm.RequestBuilder`)
- Streams are `llm.Stream` (`<-chan llm.Envelope`)
- Each envelope contains `Type`, `Meta`, and typed `Data`
- `auto` is the main zero-config convenience layer for consumers and returns `*llm.Service`
- `internal/modelcatalog` and `internal/modelview` provide built-in model metadata, aliases, and projections
- `internal/providerregistry` owns provider autodetection and build definitions
- `msg` contains the canonical message model and builders
- `usage` and `tokencount` handle pricing, usage tracking, drift, and estimation
- Tool calling centers on `tool.NewSpec`, `tool.Handle`, and `tool.Set`
- `llm.NewEventProcessor(ctx, stream)` is the main stream-consumption helper

---

## Code Style Guidelines

### Imports

```go
import (
    // 1. Standard library (alphabetical)
    "context"
    "encoding/json"
    "fmt"

    // 2. Third-party dependencies (alphabetical)
    "github.com/some/dep"

    // 3. Internal packages (alphabetical)
    "github.com/codewandler/llm"
    "github.com/codewandler/llm/auto"
)
```

- Always use full module paths: `github.com/codewandler/llm`
- No import aliasing unless absolutely necessary
- Separate groups with blank lines

### Naming Conventions

**Files:** Lowercase, descriptive, singular form
- `provider.go`, `stream.go`, `anthropic.go`

**Packages:** Lowercase, single word, matching directory
- `package llm`, `package anthropic`, `package auto`

**Types:** PascalCase, descriptive
- `Provider`, `StreamEvent`, `ProviderError`

**Functions/Methods:**
- Exported: PascalCase (`CreateStream`, `FetchModels`, `GetAccessToken`)
- Unexported: camelCase (`buildRequest`, `parseStream`)
- Constructors: `New()` or `New{Type}()` with sensible defaults

**Variables:** camelCase
- Standard: `ctx`, `opts`, `req`, `resp`, `err`, `cfg`
- Receivers: single letter (`p *Provider`, `s *Service`)

**Constants:** camelCase unexported, PascalCase exported. No SCREAMING_SNAKE_CASE.

### Constructor Pattern

```go
func New(opts ...llm.Option) *Provider {
    allOpts := append(DefaultOptions(), opts...)
    cfg := llm.Apply(allOpts...)
    return &Provider{opts: cfg, client: &http.Client{}}
}
```

For the high-level runtime, prefer `llm.New(opts...)` which returns `*llm.Service`.

### Error Handling

Use `*ProviderError` for all stream errors — never raw `error` in `StreamEvent.Error`:

```go
events.Error(llm.NewErrAPIError("anthropic", resp.StatusCode, body))
events.Error(llm.NewErrProviderMsg("anthropic", "context cancelled"))
```

Wrap non-stream errors with `%w`:
```go
return nil, fmt.Errorf("anthropic request: %w", err)
```

### Streaming and Event Publishing

Providers should emit `llm.Stream` values using `llm.NewEventPublisher()`:

```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
    req, err := src.BuildRequest(ctx)
    if err != nil {
        return nil, err
    }

    pub, stream := llm.NewEventPublisher()
    go func() {
        defer resp.Body.Close()

        pub.Publish(&llm.StreamStartedEvent{
            Model:     responseModel,
            RequestID: responseID,
            Provider:  p.Name(),
        })

        pub.Publish(&llm.DeltaEvent{
            Kind: llm.DeltaKindText,
        })

        pub.Publish(&llm.StreamCompletedEvent{
            StopReason: llm.StopReasonEndTurn,
        })
    }()
    return stream, nil
}
```

Key rules:
- Publish typed events through `llm.Publisher`; do not build ad-hoc envelopes manually unless necessary
- Streams emit `llm.Envelope` values carrying `Type`, `Meta`, and typed `Data`
- Common event types include `started`, `usage`, `token_estimate`, `delta`, `tool_call`, `completed`, `error`, and `request`
- Preserve upstream metadata such as provider name, request ID, model ID, and rate-limit info when available
- Use `llm.ProviderRequestFromHTTP()` when capturing outbound request metadata for `request` events

### Delta Events

Text tokens:
```go
pub.Publish(&llm.DeltaEvent{Kind: llm.DeltaKindText})
```

Reasoning tokens:
```go
pub.Publish(&llm.DeltaEvent{Kind: llm.DeltaKindThinking})
```

Tool argument fragments and completed tool calls:
```go
pub.Publish(&llm.DeltaEvent{Kind: llm.DeltaKindTool})
pub.Publish(&llm.ToolCallEvent{Call: toolCall})
```

`Seq` and timing metadata are stamped automatically by the publisher. Do not set envelope sequencing data manually.

---

## Domain-Specific Patterns

### Provider Interface

```go
type Provider interface {
    llm.Named
    llm.ModelsProvider
    llm.Streamer
}

type ModelsProvider interface {
    Models() llm.Models
}

type Streamer interface {
    CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error)
}

type Buildable interface {
    BuildRequest(ctx context.Context) (llm.Request, error)
}

// Optional: dynamic model listing
type ModelFetcher interface {
    FetchModels(ctx context.Context) ([]llm.Model, error)
}
```

Notes:
- `Models()` returns `llm.Models`, not `[]llm.Model`
- Providers should accept either a concrete `llm.Request` or a `*llm.RequestBuilder`
- Prefer `llm.Models` helpers for alias and ID resolution

### Requests and Builders

Use `llm.Request` as the canonical provider input. For complex construction, prefer `llm.NewRequestBuilder()`:

```go
req, err := llm.NewRequestBuilder().
    Model("claude-sonnet-4-6").
    System("You are helpful.").
    User("Explain Go channels.").
    Build()
```

Important request fields contributors should account for:
- `ThinkingMode`: `auto`, `on`, `off`
- `Effort`: `low`, `medium`, `high`, `max`
- `ApiTypeHint`: `auto`, `openai-chat`, `openai-responses`, `anthropic-messages`
- Prompt caching via `llm.CacheHint` / `msg.CacheHint`
- Tool choice via `llm.ToolChoiceAuto`, `llm.ToolChoiceRequired`, `llm.ToolChoiceNone`, `llm.ToolChoiceTool`

Providers should normalize unsupported options explicitly rather than ignoring them silently.

### Model Catalog and Usage

- Use the `catalog` package for built-in or merged model metadata
- Prefer catalog-backed aliases and pricing when adding or updating model lists
- Use `usage` for pricing, usage records, drift, and budget logic
- Put generic token estimation in `tokencount`; keep provider packages focused on provider-specific behavior

### Tool Definition Pattern

**Type-safe with handler dispatch (recommended for agentic loops):**
```go
type SearchParams struct {
    Query string `json:"query" jsonschema:"description=Search query,required"`
    Limit int    `json:"limit" jsonschema:"description=Max results,minimum=1,maximum=50"`
}

spec := tool.NewSpec[SearchParams]("search", "Search the web")

result := llm.NewEventProcessor(ctx, stream).
    HandleTool(tool.Handle(spec, func(ctx context.Context, p SearchParams) (*SearchResult, error) {
        return doSearch(p.Query, p.Limit)
    })).
    Result()
```

**Type-safe with manual dispatch:**
```go
toolset := tool.NewSet(
    tool.NewSpec[SearchParams]("search", "Search the web"),
)

stream, _ := p.CreateStream(ctx, llm.StreamRequest{Tools: toolset.Definitions(), ...})

calls, _ := toolset.Parse(rawToolCalls)
for _, call := range calls {
    switch c := call.(type) {
    case *tool.TypedToolCall[SearchParams]:
        // c.Params.Query is strongly typed
    }
}
```

**Simple definition without typed dispatch:**
```go
tool := tool.DefinitionFor[SearchParams]("search", "Search the web")
```

### Tool Call Accumulation (in stream parsers)

For streaming APIs that send tool call arguments incrementally:
```go
type toolBlock struct {
    id      string
    name    string
    jsonBuf strings.Builder
}

activeTools := make(map[int]*toolBlock)

// On argument fragment:
activeTools[index].jsonBuf.WriteString(fragment)

// On block close:
var args map[string]any
_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
es.ToolCall(llm.ToolCall{ID: tb.id, Name: tb.name, Arguments: args})
```

### Token Management Pattern (Claude OAuth)

```go
// TokenStore — low-level storage
type TokenStore interface {
    Get(ctx context.Context, key string) (*Token, error)
    Save(ctx context.Context, key string, token *Token) error
}

// ManagedTokenProvider — wraps TokenStore with auto-refresh
// Use NewLocalTokenProvider() for ~/.claude/.credentials.json
```

---

## Testing

### Framework

Use `testify`:
```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

require.NoError(t, err)    // stops test on failure
assert.Equal(t, want, got) // continues on failure
```

### Testing Stream Consumers

Use `llmtest` to build fake streams:
```go
import "github.com/codewandler/llm/llmtest"

ch := llmtest.SendEvents(
    llmtest.TextEvent("hello"),
    llmtest.ToolEvent("call_1", "get_weather", map[string]any{"location": "Berlin"}),
    llmtest.DoneEvent(&llm.Usage{InputTokens: 10, OutputTokens: 5}),
)
```

### Testing Stream Parsers

Use the `fake` provider or write a test against a recorded SSE fixture:
```go
body := io.NopCloser(strings.NewReader(sseFixture))
es := llm.NewEventStream()
go parseStream(ctx, body, es, opts)

var events []llm.StreamEvent
for ev := range es.C() {
    events = append(events, ev)
}

require.Equal(t, llm.StreamEventStart, events[0].Type)
assert.Equal(t, "claude-sonnet-4-5", events[0].Start.Model)
```

### Test Organization

- Provider-specific tests live alongside the provider package
- Cross-provider and environment-sensitive tests live under `integration/`
- Prefer smoke-style integration coverage for optional credentials and local runtimes
- Always test error paths
- Use `t.Run` for subtests, table-driven where appropriate
- For routed or multi-wire providers, assert emitted `request` events, resolved API type, and upstream provider metadata where relevant
- Prefer `msg` builders and `llm.NewRequestBuilder()` for request-construction tests; use `llmtest` for stream-consumer tests

---

## Git Workflow

- **NEVER commit without explicit user instruction**
- Check git history before committing: `git log --oneline -10`
- Ticket references go in `Refs:` tagline at end of commit body, NOT in title
- Follow repository's commit conventions (check existing commits for style)
