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

# Build a specific package
go build ./provider/anthropic

# Check for compilation errors without building binaries
go build -o /dev/null ./...
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a single test file
go test ./provider/anthropic -v

# Run a specific test function
go test -v -run TestFunctionName ./provider/anthropic

# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Linting and Formatting
```bash
# Format all code (always do this before committing)
go fmt ./...

# Tidy dependencies
go mod tidy

# Verify dependencies
go mod verify

# Vet code for suspicious constructs
go vet ./...

# Install and run golangci-lint (if available)
golangci-lint run
```

### Quick Testing with llmcli

The `cmd/llmcli` tool provides quick testing of inference and OAuth:

```bash
# Check auth status (uses ~/.claude/.credentials.json automatically)
go run ./cmd/llmcli auth status

# Quick inference test
go run ./cmd/llmcli infer "Hello"

# Verbose output with model resolution, tokens, cost, and timing
go run ./cmd/llmcli infer -v -m default "Explain Go channels"

# Model aliases: fast (haiku), default (sonnet), powerful (opus)
go run ./cmd/llmcli infer -m powerful "Complex task"
```

---

## Project Architecture

This is a **provider-based LLM abstraction layer** in Go:

```
llm/                          # Root package - core domain types
├── llm.go                    # Core types: Message, Role, ToolCall, Model
├── tool.go                   # Tool definition types
├── provider/                 # Provider abstraction
│   ├── provider.go           # Provider interface, SendOptions, StreamEvent
│   ├── registry.go           # Provider registry and model resolution
│   ├── aggregate/            # Multi-provider aggregation with failover
│   ├── anthropic/            # Direct Anthropic API
│   │   └── claude/           # OAuth-based Claude provider (token management)
│   ├── bedrock/              # AWS Bedrock
│   ├── openai/               # OpenAI GPT models
│   ├── openrouter/           # OpenRouter proxy
│   ├── ollama/               # Ollama local models
│   └── fake/                 # Test provider
└── cmd/llmcli/               # CLI tool for testing and OAuth management
    ├── cmds/                 # Command implementations (auth, infer)
    └── store/                # Token storage implementation
```

**Key concepts:**
- All LLM providers implement `provider.Provider` interface
- Communication happens via streaming channels: `<-chan provider.StreamEvent`
- Registry pattern for managing multiple providers: `registry.ResolveModel("anthropic/claude-sonnet")`
- Tool calling support through `llm.ToolDefinition` and `llm.ToolCall`
- Aggregate provider enables failover routing and model aliases (`fast`, `default`, `powerful`)

---

## Code Style Guidelines

### Imports

**Order and formatting:**
```go
import (
    // 1. Standard library (alphabetical)
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    
    // 2. Third-party dependencies (alphabetical)
    "github.com/codewandler/cc-sdk-go/oai"
    
    // 3. Internal packages (alphabetical, relative to module root)
    "github.com/codewandler/llm"
    "github.com/codewandler/llm/provider"
)
```

- Always use full module paths: `github.com/codewandler/llm`
- No import aliasing unless absolutely necessary
- Separate groups with blank lines

### Naming Conventions

**Files:** Lowercase, descriptive, singular form
- `provider.go`, `registry.go`, `anthropic.go`, `cc.go`

**Packages:** Lowercase, single word, matching directory
- `package llm`, `package provider`, `package anthropic`

**Types:** PascalCase, descriptive
- `Provider`, `StreamEvent`, `ToolCallStatus`

**Functions/Methods:**
- Exported: PascalCase (`SendMessage`, `FetchModels`, `GetAccessToken`)
- Unexported: camelCase (`buildRequest`, `parseStream`, `randomUUID`)
- Constructors: Always `New()` or `New{Type}()` with sensible defaults

**Variables:** camelCase, often abbreviated
- Standard: `ctx`, `opts`, `req`, `resp`, `err`, `cfg`
- Receivers: Single letter (`p *Provider`, `r *Registry`)
- Descriptive for complex logic: `activeTools`, `sessionID`

**Constants:** camelCase for unexported, PascalCase for exported
- No SCREAMING_SNAKE_CASE

### Types and Structs

**Constructor pattern with functional options:**
```go
// Default options exported for visibility/extension
func DefaultOptions() []llm.Option {
    return []llm.Option{
        llm.WithBaseURL(defaultBaseURL),
    }
}

// New applies defaults then user options
func New(opts ...llm.Option) *Provider {
    allOpts := append(DefaultOptions(), opts...)
    cfg := llm.Apply(allOpts...)
    return &Provider{
        opts:   cfg,
        client: &http.Client{},
    }
}

// Usage examples:
// openai.New(llm.WithAPIKey("sk-..."))
// openai.New(llm.APIKeyFromEnv("OPENAI_KEY"))
// openai.New(llm.WithAPIKeyFunc(secretStore.Get))
```

**JSON tags:** Use snake_case
```go
type Message struct {
    ID        string `json:"id,omitempty"`
    ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
```

### Error Handling

**Return errors, don't panic** (unless truly exceptional):
```go
if err != nil {
    return nil, fmt.Errorf("anthropic request: %w", err)  // Use %w for wrapping
}
```

**Error messages:**
- Lowercase first letter
- Include context before error
- Provider name as prefix: `"anthropic request: %w"`, `"token refresh: %w"`

**Sentinel errors for common cases:**
```go
var (
    ErrNotFound   = errors.New("not found")
    ErrBadRequest = errors.New("bad request")
)
```

**HTTP error handling pattern:**
```go
if resp.StatusCode != http.StatusOK {
    defer resp.Body.Close()
    errBody, _ := io.ReadAll(resp.Body)
    return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(errBody))
}
```

### Channel-Based Streaming

**Consistent streaming pattern:**
```go
func (p *Provider) SendMessage(ctx context.Context, opts provider.SendOptions) (<-chan provider.StreamEvent, error) {
    // Setup...
    
    events := make(chan provider.StreamEvent, 64)  // Buffered channel
    go parseStream(resp.Body, events)
    return events, nil
}

func parseStream(body io.ReadCloser, events chan<- provider.StreamEvent) {
    defer close(events)  // Always close channel
    defer body.Close()   // Always close body
    
    // Process stream...
}
```

### Comments

**All exported declarations need comments:**
```go
// Provider is the interface each LLM backend must implement.
type Provider interface { ... }

// New creates a new Anthropic provider.
func New(...) *Provider { ... }
```

**Comment style:**
- Start with the name being documented
- Full sentence with period
- Explain "why" not "what" for inline comments

**Section markers for long files:**
```go
// --- Request building ---
// --- SSE stream parsing ---
```

### Formatting

**Defer for cleanup:**
```go
defer resp.Body.Close()
defer close(events)
```

**Early returns:**
```go
if err != nil {
    return nil, err
}
// Continue happy path
```

**Blank lines:**
- One between functions
- Use to separate logical sections within functions
- None after `{` or before `}`

---

## Domain-Specific Patterns

### Provider Interface Implementation

Every provider must implement:
```go
type Provider interface {
    Name() string
    Models() []llm.Model
    SendMessage(ctx context.Context, opts SendOptions) (<-chan StreamEvent, error)
}
```

Optional interface for dynamic models:
```go
type ModelFetcher interface {
    FetchModels(ctx context.Context) ([]llm.Model, error)
}
```

### Model Reference Format

Use `"provider/model"` format:
```go
provider, modelID, err := registry.ResolveModel("anthropic/claude-sonnet-4-5")
```

### SSE Stream Parsing

Use large buffers and consistent structure:
```go
scanner := bufio.NewScanner(body)
scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)  // 64KB initial, 1MB max

for scanner.Scan() {
    line := scanner.Text()
    if !strings.HasPrefix(line, "data: ") {
        continue
    }
    data := strings.TrimPrefix(line, "data: ")
    // Parse JSON...
}
```

### Tool Definition Pattern

**Recommended: Use `ToolSpec` + `ToolSet` for type-safe dispatch:**
```go
type GetWeatherParams struct {
    Location string `json:"location" jsonschema:"description=City name,required"`
    Unit     string `json:"unit" jsonschema:"description=Temperature unit,enum=celsius,enum=fahrenheit"`
}

tools := llm.NewToolSet(
    llm.NewToolSpec[GetWeatherParams]("get_weather", "Get weather"),
)

// Send to provider
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Tools: tools.Definitions(),
})

// Parse with validation
calls, err := tools.Parse(rawToolCalls)
for _, call := range calls {
    switch c := call.(type) {
    case *llm.TypedToolCall[GetWeatherParams]:
        // c.Params.Location is strongly typed
    }
}
```

**Alternative: `ToolDefinitionFor[T]()` for simple cases:**
```go
tool := llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get weather")
// Returns ToolDefinition, no parsing/validation
```

**Struct tags:**
- `json:"fieldName"` - parameter name
- `jsonschema:"description=..."` - parameter description
- `jsonschema:"required"` - mark as required
- `jsonschema:"enum=val1,enum=val2"` - restrict values
- `jsonschema:"minimum=X,maximum=Y"` - numeric constraints

### Tool Call Accumulation

For streaming APIs that send tool call data incrementally:
```go
type toolBlock struct {
    id      string
    name    string
    jsonBuf strings.Builder  // Accumulate JSON chunks
}

activeTools := make(map[int]*toolBlock)

// Parse when complete
var args map[string]any
_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
```

### StreamEventStart Pattern

All providers emit `StreamEventStart` as the first event with request metadata:
```go
type StreamStart struct {
    RequestID        string        // Provider request ID
    RequestedModel   string        // Model requested by caller
    ResolvedModel    string        // Model after alias resolution
    ProviderModel    string        // Actual model from API response
    TimeToFirstToken time.Duration // Time until first content token
}

// In stream parser, track timing and emit start event:
streamMeta := &provider.StreamMeta{
    RequestedModel: opts.Model,
    StartTime:      time.Now(),
}

// After receiving first content:
if !startEventSent {
    events <- provider.StreamEvent{
        Type: provider.StreamEventStart,
        Start: &llm.StreamStart{
            RequestID:        response.ID,
            ProviderModel:    response.Model,
            TimeToFirstToken: time.Since(streamMeta.StartTime),
        },
    }
    startEventSent = true
}
```

### Token Management Pattern

OAuth providers use a layered token management architecture:
```go
// TokenStore - low-level storage interface
type TokenStore interface {
    Get(ctx context.Context, key string) (*Token, error)
    Save(ctx context.Context, key string, token *Token) error
}

// TokenProvider - provides valid tokens (may refresh)
type TokenProvider interface {
    GetAccessToken(ctx context.Context) (string, error)
}

// ManagedTokenProvider - wraps TokenStore with auto-refresh
// Single implementation of cache + refresh + save logic
type ManagedTokenProvider struct {
    key        string
    store      TokenStore
    onRefresh  func(*Token)  // Optional callback
    cachedToken *Token
    mu         sync.Mutex
}
```

Key patterns:
- `LocalTokenStore` implements `TokenStore` for `~/.claude/.credentials.json`
- `NewLocalTokenProvider()` returns `*ManagedTokenProvider` backed by `LocalTokenStore`
- No duplication of refresh logic - all refresh happens in `ManagedTokenProvider`

---

## Testing

### Testing Framework

**Use testify for assertions:**
```go
import (
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestSomething(t *testing.T) {
    // require.* stops test on failure (use for setup/preconditions)
    result, err := doSomething()
    require.NoError(t, err)
    require.NotNil(t, result)
    
    // assert.* continues test on failure (use for multiple checks)
    assert.Equal(t, "expected", result.Value)
    assert.NotEmpty(t, result.Name)
    assert.Len(t, result.Items, 3)
}
```

**Common testify assertions:**
- `require.NoError(t, err)` - Fail immediately if error
- `require.NotNil(t, value)` - Fail immediately if nil
- `assert.Equal(t, expected, actual)` - Check equality
- `assert.NotEmpty(t, value)` - Check non-empty string/slice/map
- `assert.Len(t, slice, n)` - Check length
- `assert.True(t, condition)` / `assert.False(t, condition)`
- `assert.Contains(t, haystack, needle)` - Check substring/element

### Testing Patterns

**Table-driven tests with testify:**
```go
func TestProvider(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {name: "valid input", input: "test", want: "result", wantErr: false},
        {name: "error case", input: "", want: "", wantErr: true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Process(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

**Integration testing:**
- Use `provider/fake` for testing without external dependencies
- Add all providers to `integration_test.go` table with skip flags for those requiring API keys
- Test all providers with same set of scenarios (interface, streaming, tools, conversation)

**Test organization:**
- Test files should be named `*_test.go`
- Provider-specific tests go in provider's package (e.g., `provider/fake/fake_test.go`)
- Cross-provider integration tests in root `integration_test.go`
- Always test error paths
- Write benchmarks for performance-critical paths

---

## Git Workflow

- **NEVER commit without explicit user instruction**
- Check git history before committing: `git log --oneline -10`
- Ticket references go in `Refs:` tagline at end of commit body, NOT in title
- Follow repository's commit conventions (check existing commits for style)
