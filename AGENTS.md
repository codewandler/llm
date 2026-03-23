# AGENTS.md - Developer Guide for Coding Agents

This guide is for AI coding agents working in this repository. Follow these conventions to maintain consistency.

## Communication Guidelines

- **DO NOT provide status summaries after completing tasks** - No "Summary", "Status", "Changes Made", or similar sections after every turn. Just do the work and move on. The user can see what was done from git/output.

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
```

### Linting and Formatting
```bash
go fmt ./...          # format (always do before committing)
go mod tidy           # tidy dependencies
go vet ./...          # vet for suspicious constructs
golangci-lint run     # if available
```

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
├── llm.go              # Provider interface, Streamer interface
├── stream.go           # StreamEvent, StreamRequest, Delta, EventStream, Usage
├── stream_response.go  # StreamResponse, Process(), StreamResult
├── message.go          # Message types: UserMsg, AssistantMsg, ToolCallResult, etc.
├── tool.go             # ToolDefinition, ToolSpec, ToolSet, TypedToolCall
├── errors.go           # ProviderError, error sentinels
├── model.go            # Model type
├── option.go           # Functional options (WithAPIKey, WithHTTPClient, etc.)
├── reasoning.go        # ReasoningEffort constants
├── llmtest/            # Test helpers (SendEvents, TextEvent, etc.)
│
└── provider/
    ├── anthropic/      # Direct Anthropic API
    │   └── claude/     # OAuth-based Claude provider (token management)
    ├── bedrock/        # AWS Bedrock
    ├── openai/         # OpenAI API (Chat Completions + Responses API)
    ├── openrouter/     # OpenRouter proxy
    ├── ollama/         # Local Ollama
    ├── auto/           # Zero-config multi-provider setup
    ├── router/         # Multi-provider routing with failover
    └── fake/           # Test provider
```

**Key concepts:**
- All LLM providers implement `llm.Provider` (`CreateStream`, `Name`, `Models`)
- Streams are `<-chan llm.StreamEvent` — every event is stamped with `RequestID`, `Seq`, `Timestamp`
- `provider/auto` is the primary entry point for consumers: `auto.New(ctx, ...Option)`
- `provider/router` handles multi-provider routing, failover, and alias resolution
- Tool calling: `ToolSpec` + `ToolSet` for type-safe parse/dispatch; `ToolDefinitionFor[T]()` for simple cases
- `StreamResponse` / `Process()` for agentic tool-dispatch loops with typed handlers

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
    "github.com/codewandler/llm/provider/auto"
)
```

- Always use full module paths: `github.com/codewandler/llm`
- No import aliasing unless absolutely necessary
- Separate groups with blank lines

### Naming Conventions

**Files:** Lowercase, descriptive, singular form
- `provider.go`, `stream.go`, `anthropic.go`

**Packages:** Lowercase, single word, matching directory
- `package llm`, `package router`, `package anthropic`

**Types:** PascalCase, descriptive
- `Provider`, `StreamEvent`, `ProviderError`

**Functions/Methods:**
- Exported: PascalCase (`CreateStream`, `FetchModels`, `GetAccessToken`)
- Unexported: camelCase (`buildRequest`, `parseStream`)
- Constructors: `New()` or `New{Type}()` with sensible defaults

**Variables:** camelCase
- Standard: `ctx`, `opts`, `req`, `resp`, `err`, `cfg`
- Receivers: single letter (`p *Provider`, `r *Router`)

**Constants:** camelCase unexported, PascalCase exported. No SCREAMING_SNAKE_CASE.

### Constructor Pattern

```go
func New(opts ...llm.Option) *Provider {
    allOpts := append(DefaultOptions(), opts...)
    cfg := llm.Apply(allOpts...)
    return &Provider{opts: cfg, client: &http.Client{}}
}
```

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

### Channel-Based Streaming

All providers must use `EventStream`:

```go
func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
    // ... build and send request ...

    es := llm.NewEventStream()
    go p.parseStream(ctx, resp.Body, es, opts)
    return es.C(), nil
}

func (p *Provider) parseStream(ctx context.Context, body io.ReadCloser, es *llm.EventStream, opts llm.StreamRequest) {
    defer es.Close()
    defer body.Close()

    var startEmitted bool
    for scanner.Scan() {
        // ... parse SSE ...

        if !startEmitted {
            startEmitted = true
            es.Start(llm.StreamStartOpts{
                Model:     responseModel,
                RequestID: responseID,
            })
        }
        es.Delta(llm.TextDelta(nil, text))
    }
    es.Done(usage)
}
```

Key rules:
- Always call `es.Close()` via defer
- Emit `StreamEventStart` before the first content event (or at `response.completed` if no content)
- Use `es.Start()`, `es.Delta()`, `es.ToolCall()`, `es.Done()`, `es.Error()` — not `es.Send()` directly
- Use large scanner buffers: `scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)`

### Delta Events

Text tokens:
```go
es.Delta(llm.TextDelta(nil, text))
es.Delta(llm.TextDelta(llm.DeltaIndex(i), text)) // with block index
```

Reasoning tokens:
```go
es.Delta(llm.ReasoningDelta(nil, thinkingText))
```

Tool argument fragments (streaming):
```go
es.Delta(llm.ToolDelta(llm.DeltaIndex(i), id, name, argsFragment))
```

Completed tool calls:
```go
es.ToolCall(llm.ToolCall{ID: id, Name: name, Arguments: args})
```

### StreamStart Pattern

```go
es.Start(llm.StreamStartOpts{
    Model:     responseModel,  // model ID from the API response
    RequestID: responseID,     // request ID from the API response
})
```

`RequestID`, `Seq`, and `Timestamp` are stamped automatically by `EventStream`. Do not set them manually.

---

## Domain-Specific Patterns

### Provider Interface

```go
type Provider interface {
    Name() string
    Models() []llm.Model
    CreateStream(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error)
}

// Optional: dynamic model listing
type ModelFetcher interface {
    FetchModels(ctx context.Context) ([]llm.Model, error)
}
```

### Tool Definition Pattern

**Type-safe with handler dispatch (recommended for agentic loops):**
```go
type SearchParams struct {
    Query string `json:"query" jsonschema:"description=Search query,required"`
    Limit int    `json:"limit" jsonschema:"description=Max results,minimum=1,maximum=50"`
}

spec := llm.NewToolSpec[SearchParams]("search", "Search the web")

result := <-llm.Process(ctx, stream).
    HandleTool(llm.Handle(spec, func(ctx context.Context, p SearchParams) (*SearchResult, error) {
        return doSearch(p.Query, p.Limit)
    })).
    Result()
```

**Type-safe with manual dispatch:**
```go
toolset := llm.NewToolSet(
    llm.NewToolSpec[SearchParams]("search", "Search the web"),
)

stream, _ := p.CreateStream(ctx, llm.StreamRequest{Tools: toolset.Definitions(), ...})

calls, _ := toolset.Parse(rawToolCalls)
for _, call := range calls {
    switch c := call.(type) {
    case *llm.TypedToolCall[SearchParams]:
        // c.Params.Query is strongly typed
    }
}
```

**Simple definition without typed dispatch:**
```go
tool := llm.ToolDefinitionFor[SearchParams]("search", "Search the web")
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

- Provider-specific tests: `provider/anthropic/anthropic_test.go`
- Cross-provider integration tests: `integration_test.go`
- Always test error paths
- Use `t.Run` for subtests, table-driven where appropriate

---

## Git Workflow

- **NEVER commit without explicit user instruction**
- Check git history before committing: `git log --oneline -10`
- Ticket references go in `Refs:` tagline at end of commit body, NOT in title
- Follow repository's commit conventions (check existing commits for style)
