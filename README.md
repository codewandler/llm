# LLM Provider Abstraction Library

A unified Go library for interacting with multiple LLM providers through a consistent interface. Supports streaming responses, tool calling, reasoning, prompt caching, and zero-config multi-provider setup.

## Features

- **Unified Provider Interface** — Single API for multiple LLM providers
- **Streaming Support** — Channel-based streaming with structured delta events
- **Tool Calling** — Consistent tool/function calling across providers
- **Typed Tool Dispatch** — `StreamResponse` handles tool calls with strongly-typed handlers
- **Reasoning Support** — Extended thinking / reasoning tokens (Anthropic, OpenAI o-series, Bedrock)
- **Prompt Caching** — Transparent cache control for Anthropic, Bedrock, and OpenAI
- **Context Cancellation** — Proper cancellation support for long-running streams
- **Zero-config Setup** — `provider/auto` auto-detects providers from environment variables
- **`llmtest` Package** — Test helpers for stream consumers (`net/http/httptest` style)

## Supported Providers

| Provider | Name | Description |
|----------|------|-------------|
| Anthropic API | `anthropic` | Direct Anthropic API with API key |
| Claude OAuth | `claude` | OAuth-based Claude access (auto-detects local credentials) |
| OpenAI | `openai` | OpenAI GPT models (GPT-4o, GPT-5, o-series, Codex) |
| AWS Bedrock | `bedrock` | AWS Bedrock models (Claude, Llama, etc.) |
| Ollama | `ollama` | Local Ollama models |
| OpenRouter | `openrouter` | 200+ tool-enabled models via OpenRouter proxy |
| Router | `router` | Combines multiple providers with failover and aliases |

## Installation

```bash
go get github.com/codewandler/llm
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/provider/auto"
)

func main() {
    ctx := context.Background()

    // Auto-detects providers from environment variables
    p, err := auto.New(ctx)
    if err != nil {
        panic(err)
    }

    events, err := p.CreateStream(ctx, llm.StreamRequest{
        Model: "anthropic/claude-sonnet-4-5",
        Messages: llm.Messages{
            &llm.UserMsg{Content: "What is the capital of France?"},
        },
    })
    if err != nil {
        panic(err)
    }

    for event := range events {
        switch event.Type {
        case llm.StreamEventDelta:
            fmt.Print(event.Text())
        case llm.StreamEventDone:
            fmt.Println()
            if event.Usage != nil {
                fmt.Printf("Tokens: %d in, %d out\n",
                    event.Usage.InputTokens, event.Usage.OutputTokens)
            }
        case llm.StreamEventError:
            fmt.Printf("Error: %v\n", event.Error)
        }
    }
}
```

## Provider Setup

### `provider/auto` — Zero-Config Multi-Provider

`auto.New(ctx, ...Option)` auto-detects providers from environment variables
and returns a ready-to-use `llm.Provider`:

```go
import "github.com/codewandler/llm/provider/auto"

// Auto-detect everything from environment variables
p, err := auto.New(ctx)

// Or explicitly opt in to specific providers
p, err := auto.New(ctx,
    auto.WithAnthropic(),     // ANTHROPIC_API_KEY
    auto.WithOpenAI(),        // OPENAI_KEY or OPENAI_API_KEY
    auto.WithBedrock(),       // AWS credentials
    auto.WithOpenRouter(),    // OPENROUTER_API_KEY
    auto.WithClaudeLocal(),   // ~/.claude/.credentials.json
)

// Add a Claude OAuth account from a token store
p, err := auto.New(ctx, auto.WithClaude(myTokenStore))

// Custom global aliases with failover
p, err := auto.New(ctx,
    auto.WithOpenAI(),
    auto.WithOpenRouter(),
    auto.WithGlobalAlias("o3", "openai/o3", "openrouter/openai/o3"),
)
```

### Direct Provider Usage

Each provider can also be used directly without `auto`:

```go
import "github.com/codewandler/llm/provider/anthropic"

p := anthropic.New(llm.APIKeyFromEnv("ANTHROPIC_API_KEY"))

events, err := p.CreateStream(ctx, llm.StreamRequest{
    Model:    "claude-sonnet-4-5",
    Messages: llm.Messages{&llm.UserMsg{Content: "Hello!"}},
})
```

```go
import "github.com/codewandler/llm/provider/openai"

p := openai.New(llm.APIKeyFromEnv("OPENAI_KEY"))
```

```go
import "github.com/codewandler/llm/provider/bedrock"

p := bedrock.New() // uses default AWS credential chain
p := bedrock.New(bedrock.WithRegion("us-east-1"))
```

```go
import "github.com/codewandler/llm/provider/ollama"

p := ollama.New("http://localhost:11434")
```

```go
import "github.com/codewandler/llm/provider/openrouter"

p := openrouter.New(llm.APIKeyFromEnv("OPENROUTER_API_KEY"))
```

### Claude OAuth Provider

```go
import "github.com/codewandler/llm/provider/anthropic/claude"

// Auto-detect local Claude credentials (default)
p := claude.New()

// Or with explicit token provider
p := claude.New(
    claude.WithManagedTokenProvider("my-key", tokenStore, nil),
)
```

Token management interfaces:
- `TokenStore` — stores and retrieves tokens (implement for your storage backend)
- `LocalTokenStore` — reads from `~/.claude/.credentials.json`
- `ManagedTokenProvider` — wraps a TokenStore with automatic refresh

### Router Provider

For custom multi-provider routing with failover:

```go
import "github.com/codewandler/llm/provider/router"

p, err := router.New(cfg, factories)
```

## Stream Events

```go
type StreamEvent struct {
    // Metadata stamped on every event
    RequestID string
    Seq       uint64
    Timestamp time.Time

    Type     StreamEventType
    Start    *StreamStart  // StreamEventStart
    Delta    *Delta        // StreamEventDelta
    ToolCall *ToolCall     // StreamEventToolCall
    Routed   *Routed       // StreamEventRouted
    Usage    *Usage        // StreamEventDone
    Error    *ProviderError // StreamEventError
}

const (
    StreamEventCreated  // emitted immediately when stream is opened
    StreamEventStart    // first content event; carries request metadata
    StreamEventDelta    // text or reasoning token
    StreamEventToolCall // completed tool call
    StreamEventRouted   // router selected a backend
    StreamEventDone     // stream complete; carries usage
    StreamEventError    // error occurred
)
```

### Reading Deltas

```go
for event := range stream {
    switch event.Type {
    case llm.StreamEventDelta:
        fmt.Print(event.Text())          // text tokens
        fmt.Print(event.ReasoningText()) // reasoning tokens (thinking models)
    }
}
```

`event.Delta` is a `*Delta` struct:

```go
type Delta struct {
    Type      DeltaType // DeltaTypeText | DeltaTypeReasoning
    Text      string
    Reasoning string
    Index     *uint32   // block index, provider-dependent
}
```

### StreamStart Metadata

```go
case llm.StreamEventStart:
    fmt.Printf("Request ID: %s\n", event.Start.RequestID)
    fmt.Printf("Model: %s\n", event.Start.Model)
    fmt.Printf("TTFT: %s\n", event.Start.TimeToFirstToken)
```

### Error Handling

```go
events, err := p.CreateStream(ctx, req)
if err != nil {
    // Initial request failed (auth, bad params, etc.)
}

for event := range events {
    if event.Type == llm.StreamEventError {
        if errors.Is(event.Error, llm.ErrAPIError) {
            fmt.Printf("API error %d: %s\n", event.Error.StatusCode, event.Error.Body)
        }
        if errors.Is(event.Error, llm.ErrContextCancelled) {
            fmt.Println("cancelled")
        }
    }
}
```

Error sentinels: `ErrContextCancelled`, `ErrRequestFailed`, `ErrAPIError`,
`ErrStreamRead`, `ErrStreamDecode`, `ErrMissingAPIKey`, `ErrBuildRequest`,
`ErrUnknownModel`, `ErrNoProviders`.

### Usage Information

```go
case llm.StreamEventDone:
    if event.Usage != nil {
        fmt.Printf("in=%d out=%d cost=$%.6f\n",
            event.Usage.InputTokens, event.Usage.OutputTokens, event.Usage.Cost)
        fmt.Printf("cache read=%d write=%d\n",
            event.Usage.CacheReadTokens, event.Usage.CacheWriteTokens)
        fmt.Printf("reasoning=%d\n", event.Usage.ReasoningTokens)
    }
```

## Tool Calling

### Typed Tool Dispatch with `StreamResponse`

The recommended approach for agentic tool use. `Process` accumulates the stream
and dispatches tool calls to typed handlers:

```go
type GetWeatherParams struct {
    Location string `json:"location" jsonschema:"description=City name,required"`
    Unit     string `json:"unit"     jsonschema:"description=Unit,enum=celsius,enum=fahrenheit"`
}

type GetWeatherResult struct {
    Temp int    `json:"temp"`
    Desc string `json:"desc"`
}

spec := llm.NewToolSpec[GetWeatherParams]("get_weather", "Get current weather")

result := <-llm.Process(ctx, stream).
    HandleTool(llm.Handle(spec, func(ctx context.Context, p GetWeatherParams) (*GetWeatherResult, error) {
        return &GetWeatherResult{Temp: 22, Desc: "sunny"}, nil
    })).
    Result()

fmt.Println(result.Text)
fmt.Println(result.ToolResults) // []ToolCallResult ready to append to messages
```

For concurrent tool execution:

```go
result := <-llm.Process(ctx, stream).
    DispatchAsync().
    HandleTool(...).
    Result()
```

Callback hooks:

```go
result := <-llm.Process(ctx, stream).
    OnText(func(s string) { fmt.Print(s) }).
    OnReasoning(func(s string) { /* handle thinking */ }).
    OnStart(func(s *llm.StreamStart) { log.Println(s.RequestID) }).
    HandleTool(...).
    Result()
```

`StreamResult` fields: `Text`, `Reasoning`, `ToolCalls`, `ToolResults`, `Usage`, `StopReason`, `Start`.

### Low-Level Tool Definitions

For manual tool management:

```go
tools := []llm.ToolDefinition{
    llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get current weather"),
}

events, err := p.CreateStream(ctx, llm.StreamRequest{
    Model:    "anthropic/claude-sonnet-4-5",
    Messages: messages,
    Tools:    tools,
})

for event := range events {
    if event.Type == llm.StreamEventToolCall {
        tc := event.ToolCall
        // tc.ID, tc.Name, tc.Arguments (map[string]any)
    }
}
```

### ToolSet — Type-Safe Parse + Dispatch

```go
toolset := llm.NewToolSet(
    llm.NewToolSpec[GetWeatherParams]("get_weather", "Get weather"),
)

stream, _ := p.CreateStream(ctx, llm.StreamRequest{
    Tools: toolset.Definitions(),
    ...
})

// collect raw tool calls from stream, then:
calls, err := toolset.Parse(rawToolCalls)
for _, call := range calls {
    switch c := call.(type) {
    case *llm.TypedToolCall[GetWeatherParams]:
        fmt.Println(c.Params.Location) // strongly typed
    }
}
```

### Tool Choice

```go
// Model decides (default)
llm.StreamRequest{ToolChoice: llm.ToolChoiceAuto{}}

// Must call at least one tool
llm.StreamRequest{ToolChoice: llm.ToolChoiceRequired{}}

// Cannot call any tools
llm.StreamRequest{ToolChoice: llm.ToolChoiceNone{}}

// Must call a specific tool
llm.StreamRequest{ToolChoice: llm.ToolChoiceTool{Name: "get_weather"}}
```

| Type | OpenAI | Anthropic | Ollama |
|------|--------|-----------|--------|
| `ToolChoiceAuto{}` | `"auto"` | `{"type":"auto"}` | ignored |
| `ToolChoiceRequired{}` | `"required"` | `{"type":"any"}` | ignored |
| `ToolChoiceNone{}` | `"none"` | omitted | ignored |
| `ToolChoiceTool{Name:"X"}` | `{"type":"function",...}` | `{"type":"tool","name":"X"}` | ignored |

### Struct Tag Reference

```go
type Params struct {
    Location string  `json:"location" jsonschema:"description=City name,required"`
    Unit     string  `json:"unit"     jsonschema:"description=Unit,enum=celsius,enum=fahrenheit"`
    Limit    int     `json:"limit"    jsonschema:"minimum=1,maximum=100"`
    Pattern  string  `json:"pattern"  jsonschema:"pattern=^[a-z]+$"`
}
```

## Messages

```go
var msgs llm.Messages
msgs.AddSystemMsg("You are helpful.")
msgs.AddUserMsg("Hello")
msgs.AddAssistantMsg("Hi there")
msgs.AddToolCallResult(callID, output, false /* isError */)
msgs.Append(msg)
```

Or construct inline:

```go
msgs := llm.Messages{
    &llm.SystemMsg{Content: "You are helpful."},
    &llm.UserMsg{Content: "Hello"},
    &llm.AssistantMsg{ToolCalls: []llm.ToolCall{tc}},
    &llm.ToolCallResult{ToolCallID: tc.ID, Output: result},
}
```

## Reasoning Effort (OpenAI)

```go
stream, _ := p.CreateStream(ctx, llm.StreamRequest{
    Model:           "openai/o3",
    Messages:        messages,
    ReasoningEffort: llm.ReasoningEffortHigh,
})

// Reasoning tokens arrive as StreamEventDelta with DeltaTypeReasoning
for event := range stream {
    if event.Type == llm.StreamEventDelta {
        fmt.Print(event.Text())          // response text
        fmt.Print(event.ReasoningText()) // thinking tokens
    }
}
```

| Constant | Value | Notes |
|----------|-------|-------|
| `ReasoningEffortNone` | `"none"` | GPT-5.1+ only |
| `ReasoningEffortLow` | `"low"` | |
| `ReasoningEffortMedium` | `"medium"` | Default for pre-5.1 |
| `ReasoningEffortHigh` | `"high"` | |
| `ReasoningEffortXHigh` | `"xhigh"` | Codex-max+ only |

## Prompt Caching

```go
// Top-level hint: cache the entire conversation prefix
events, err := p.CreateStream(ctx, llm.StreamRequest{
    Model:     "anthropic/claude-sonnet-4-5",
    Messages:  messages,
    CacheHint: &llm.CacheHint{Enabled: true},
})

// Per-message breakpoints (advanced)
msgs := llm.Messages{
    &llm.SystemMsg{
        Content:   largeSystemPrompt,
        CacheHint: &llm.CacheHint{Enabled: true},
    },
    &llm.UserMsg{Content: "Hello"},
}
```

| Provider | Mode | TTL options |
|---|---|---|
| **Anthropic** | Explicit breakpoints | 5 min (default), 1 h (selected models) |
| **Bedrock** (Claude) | Explicit breakpoints | 5 min (default), 1 h (selected models) |
| **OpenAI** | Fully automatic | in-memory (default), 1 h via `CacheHint{TTL:"1h"}` |
| **Ollama / OpenRouter** | Not supported | — |

Cache usage is reported in `event.Usage.CacheReadTokens` / `CacheWriteTokens`.

## Model Reference Format

```
anthropic/claude-sonnet-4-5           # Direct Anthropic API
claude/claude-sonnet-4-5              # Claude OAuth provider
openai/gpt-4o                         # OpenAI
bedrock/anthropic.claude-3-5-sonnet   # AWS Bedrock
ollama/llama3.2:1b                    # Local Ollama
openrouter/anthropic/claude-sonnet-4.5  # OpenRouter proxy
```

Global aliases (configured via `auto.WithGlobalAlias`):
- `fast` — fastest/cheapest model
- `default` — balanced performance
- `powerful` — most capable model
- `codex` — OpenAI Codex model

## Testing with `llmtest`

```go
import "github.com/codewandler/llm/llmtest"

ch := llmtest.SendEvents(
    llmtest.TextEvent("hello"),
    llmtest.ToolEvent("call_1", "get_weather", map[string]any{"location": "Berlin"}),
    llmtest.DoneEvent(nil),
)

result := <-llm.Process(ctx, ch).HandleTool(...).Result()
```

Functions: `SendEvents`, `TextEvent`, `ReasoningEvent`, `ToolEvent`, `DoneEvent`, `ErrorEvent`.

## Context Cancellation

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

events, err := p.CreateStream(ctx, llm.StreamRequest{...})
for event := range events {
    if event.Type == llm.StreamEventError {
        if errors.Is(event.Error, llm.ErrContextCancelled) {
            fmt.Println("timed out")
        }
    }
}
```

## Environment Variables

```bash
export ANTHROPIC_API_KEY="your-api-key"
export OPENAI_KEY="your-api-key"
export OPENROUTER_API_KEY="your-api-key"
export OLLAMA_BASE_URL="http://localhost:11434"  # optional, default shown
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
```

## Architecture

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
    │   └── claude/     # OAuth-based Claude provider
    ├── bedrock/        # AWS Bedrock
    ├── openai/         # OpenAI API (Chat + Responses API)
    ├── openrouter/     # OpenRouter proxy
    ├── ollama/         # Local Ollama
    ├── auto/           # Zero-config multi-provider setup
    ├── router/         # Multi-provider routing with failover
    └── fake/           # Test provider
```

## CLI Tool

```bash
go run ./cmd/llmcli auth status          # Check Claude OAuth credentials
go run ./cmd/llmcli infer "Hello"        # Quick inference test
go run ./cmd/llmcli infer -v -m default "Explain Go channels"  # Verbose
```

## Contributing

```bash
go test ./...         # run all tests
go test -race ./...   # race detector
go fmt ./...          # format
go vet ./...          # vet
```

See [AGENTS.md](AGENTS.md) for architecture and coding conventions.

## License

MIT — see [LICENSE](LICENSE).
