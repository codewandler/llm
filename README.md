# LLM Provider Abstraction Library

A unified Go library for interacting with multiple LLM providers through a consistent interface. Supports streaming responses, tool calling, and automatic provider registration.

## Features

- **Unified Provider Interface** - Single API for multiple LLM providers
- **Streaming Support** - Channel-based streaming for all providers
- **Tool Calling** - Consistent tool/function calling across providers
- **Context Cancellation** - Proper cancellation support for long-running streams
- **Registry Pattern** - Automatic provider discovery with `provider/model` format
- **Production Ready** - Race-free, tested with comprehensive integration tests

## Supported Providers

| Provider | Name | Description |
|----------|------|-------------|
| Claude Code | `anthropic:claude-code` | Local Claude CLI wrapper (requires `claude` in PATH) |
| Anthropic API | `anthropic` | Direct Anthropic API with OAuth support |
| Ollama | `ollama` | Local Ollama models (11 curated defaults) |
| OpenRouter | `openrouter` | 229 tool-enabled models via OpenRouter proxy |

## Installation

```bash
go get github.com/codewandler/llm
```

## Quick Start

### Using the Default Registry

The simplest way to use the library is with the default registry:

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/provider"
)

func main() {
    // Set environment variables for providers
    os.Setenv("OPENROUTER_API_KEY", "your-api-key")
    
    ctx := context.Background()
    
    // Create a stream using provider/model format
    events, err := provider.CreateStream(ctx, llm.StreamOptions{
        Model: "ollama/glm-4.7-flash",
        Messages: []llm.Message{
            {Role: llm.RoleUser, Content: "What is the capital of France?"},
        },
    })
    if err != nil {
        panic(err)
    }
    
    // Process streaming response
    for event := range events {
        switch event.Type {
        case llm.StreamEventDelta:
            fmt.Print(event.Delta)
        case llm.StreamEventDone:
            fmt.Println("\nDone!")
            if event.Usage != nil {
                fmt.Printf("Tokens: %d input, %d output\n", 
                    event.Usage.InputTokens, event.Usage.OutputTokens)
            }
        case llm.StreamEventError:
            fmt.Printf("Error: %v\n", event.Error)
        }
    }
}
```

### Creating a Custom Registry

For more control, create your own registry:

```go
import (
    "github.com/codewandler/llm/provider"
    "github.com/codewandler/llm/provider/ollama"
    "github.com/codewandler/llm/provider/openrouter"
)

// Create empty registry
reg := llm.NewRegistry()

// Register specific providers
reg.Register(ollama.New("http://localhost:11434"))
reg.Register(openrouter.New("your-api-key"))

// Use the registry
events, err := reg.CreateStream(ctx, llm.StreamOptions{
    Model: "openrouter/anthropic/claude-sonnet-4.5",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "Hello!"},
    },
})
```

## Provider-Specific Usage

### Claude Code (CLI Wrapper)

Requires the `claude` CLI tool in your PATH:

```go
import "github.com/codewandler/llm/provider/anthropic"

provider := anthropic.NewClaudeCodeProvider()

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "sonnet",  // or "opus", "haiku"
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "Explain Go channels"},
    },
})
```

### Anthropic API (Direct)

Direct API access with OAuth token refresh:

```go
import "github.com/codewandler/llm/provider/anthropic"

provider := anthropic.New(&anthropic.Config{
    ClientID:     "your-client-id",
    ClientSecret: "your-client-secret",
    RefreshToken: "your-refresh-token",
})

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "claude-3-5-sonnet-20241022",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "Hello!"},
    },
})
```

### Ollama (Local Models)

```go
import "github.com/codewandler/llm/provider/ollama"

provider := ollama.New("http://localhost:11434")

// Download a model if needed
if err := provider.Download(ctx, "llama3.2:1b"); err != nil {
    // Handle error
}

// Use the model
events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "llama3.2:1b",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "Hello!"},
    },
})
```

**Curated Ollama Models** (all tested with tool calling):
- `glm-4.7-flash` (default)
- `ministral-3:8b`
- `rnj-1`
- `functiongemma`
- `devstral-small-2`
- `nemotron-3-nano:30b`
- `llama3.2:1b`, `qwen3:1.7b`, `qwen3:0.6b`, `granite3.1-moe:1b`, `qwen2.5:0.5b`

### OpenRouter (Multi-Provider Proxy)

Access 229 tool-enabled models:

```go
import "github.com/codewandler/llm/provider/openrouter"

provider := openrouter.New("your-api-key")

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "anthropic/claude-sonnet-4.5",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "Hello!"},
    },
})
```

Popular OpenRouter models:
- `anthropic/claude-sonnet-4.5`
- `google/gemini-2.0-flash-001`
- `openai/gpt-4-turbo`
- `meta-llama/llama-3.1-70b-instruct`

See [provider/openrouter/README.md](provider/openrouter/README.md) for full model list.

## Tool Calling

All providers support tool/function calling with automatic tool call ID tracking.

### Type-Safe Tool Dispatch (Recommended)

The best way to work with tools is using `ToolSpec` and `ToolSet`, which provide:
- Automatic JSON Schema generation from Go structs
- Runtime validation of tool arguments
- Type-safe parameter access via generics
- Clean type-switch dispatch

```go
// 1. Define parameter structs
type GetWeatherParams struct {
    Location string `json:"location" jsonschema:"description=City name,required"`
    Unit     string `json:"unit" jsonschema:"description=Temperature unit,enum=celsius,enum=fahrenheit"`
}

type SearchParams struct {
    Query string `json:"query" jsonschema:"description=Search query,required"`
    Limit int    `json:"limit" jsonschema:"description=Max results,minimum=1,maximum=100"`
}

// 2. Create ToolSet
tools := llm.NewToolSet(
    llm.NewToolSpec[GetWeatherParams]("get_weather", "Get weather for a location"),
    llm.NewToolSpec[SearchParams]("search", "Search the web"),
)

// 3. Send to LLM
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:    "openrouter/moonshotai/kimi-k2-0905",
    Messages: messages,
    Tools:    tools.Definitions(),  // Returns []ToolDefinition
})

// 4. Collect tool calls from stream
var rawCalls []llm.ToolCall
for event := range stream {
    if event.Type == llm.StreamEventToolCall {
        rawCalls = append(rawCalls, *event.ToolCall)
    }
}

// 5. Parse with validation
calls, err := tools.Parse(rawCalls)
if err != nil {
    log.Printf("parse warnings: %v", err)  // Non-fatal: you still get valid calls
}

// 6. Type-safe dispatch
for _, call := range calls {
    switch c := call.(type) {
    case *llm.TypedToolCall[GetWeatherParams]:
        // c.Params is strongly typed!
        fmt.Printf("Weather for: %s (unit: %s)\n", c.Params.Location, c.Params.Unit)
        result := getWeather(c.Params.Location, c.Params.Unit)
        
        // Send result back
        messages = append(messages,
            llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
                ID: c.ID, Name: c.Name, Arguments: map[string]any{
                    "location": c.Params.Location,
                    "unit": c.Params.Unit,
                },
            }}},
            llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: c.ID},
        )
        
    case *llm.TypedToolCall[SearchParams]:
        fmt.Printf("Search: %s (limit: %d)\n", c.Params.Query, c.Params.Limit)
        // ... handle search
    }
}
```

**Benefits:**
- Arguments are validated against JSON Schema (required fields, types, enums, ranges)
- Type-safe access: `c.Params.Location` instead of `c.Arguments["location"].(string)`
- Compile-time checking of parameter struct fields
- Parse errors are non-fatal - you get all successfully parsed calls

### Quick Example (Type-Safe with Generics)

The recommended way is using `ToolDefinitionFor[T]()` which generates JSON Schema from Go structs:

```go
// Define parameter struct with struct tags
type GetWeatherParams struct {
    Location string `json:"location" jsonschema:"description=City name or coordinates,required"`
    Unit     string `json:"unit" jsonschema:"description=Temperature unit,enum=celsius,enum=fahrenheit"`
}

// Create tool definition from struct
tools := []llm.ToolDefinition{
    llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get current weather for a location"),
}

// Step 1: Send initial request with tools
events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model:    "ollama/glm-4.7-flash",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "What's the weather in Paris?"},
    },
    Tools: tools,
})

// Step 2: Process tool calls
var toolCall *llm.ToolCall
for event := range events {
    if event.Type == llm.StreamEventToolCall {
        toolCall = event.ToolCall
        // Arguments are automatically parsed into map[string]any
        fmt.Printf("Tool: %s\n", toolCall.Name)
        fmt.Printf("Location: %s\n", toolCall.Arguments["location"])
    }
}

// Step 3: Execute the tool
result := fmt.Sprintf(`{"temp": 22, "conditions": "sunny"}`)

// Step 4: Send tool result back
events2, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "ollama/glm-4.7-flash",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "What's the weather in Paris?"},
        {
            Role:      llm.RoleAssistant,
            ToolCalls: []llm.ToolCall{*toolCall},
        },
        {
            Role:       llm.RoleTool,
            Content:    result,
            ToolCallID: toolCall.ID,  // Link result to original call
        },
    },
    Tools: tools,
})

// Step 5: Get final response
for event := range events2 {
    if event.Type == llm.StreamEventDelta {
        fmt.Print(event.Delta)
    }
}
```

### Struct Tag Reference

The `ToolDefinitionFor[T]()` function uses these tags:

- `json:"fieldName"` - Parameter name (required)
- `jsonschema:"description=..."` - Parameter description
- `jsonschema:"required"` - Mark parameter as required
- `jsonschema:"enum=val1,enum=val2"` - Restrict to specific values
- `jsonschema:"minimum=1,maximum=10"` - Numeric constraints
- `jsonschema:"pattern=^[a-z]+$"` - String pattern (regex)

### Manual Tool Definition

You can also define tools manually:

```go
tools := []llm.ToolDefinition{
    {
        Name:        "get_weather",
        Description: "Get current weather for a location",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "location": map[string]any{
                    "type":        "string",
                    "description": "City name",
                },
            },
            "required": []string{"location"},
        },
    },
}
```

**Important:** Tool result messages must include `ToolCallID` to link them to the original tool call.

## Multi-Turn Conversations

Build conversations by appending messages:

```go
messages := []llm.Message{
    {Role: llm.RoleUser, Content: "Hello!"},
}

// First turn
events, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:    "ollama/glm-4.7-flash",
    Messages: messages,
})

var response string
for event := range events {
    if event.Type == llm.StreamEventDelta {
        response += event.Delta
    }
}

// Add assistant response to history
messages = append(messages, llm.Message{
    Role:    llm.RoleAssistant,
    Content: response,
})

// Second turn
messages = append(messages, llm.Message{
    Role:    llm.RoleUser,
    Content: "Tell me more about that",
})

events, _ = provider.CreateStream(ctx, llm.StreamOptions{
    Model:    "ollama/glm-4.7-flash",
    Messages: messages,
})
```

## Context Cancellation

All stream parsers support context cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "ollama/glm-4.7-flash",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "Write a very long essay"},
    },
})

for event := range events {
    if event.Type == llm.StreamEventError {
        if errors.Is(event.Error, context.DeadlineExceeded) {
            fmt.Println("Request timed out")
        }
    }
}
```

## Environment Variables

Configure providers via environment variables:

```bash
# OpenRouter
export OPENROUTER_API_KEY="your-api-key"

# Ollama (optional, defaults to http://localhost:11434)
export OLLAMA_BASE_URL="http://localhost:11434"

# Anthropic OAuth (for direct API access)
export ANTHROPIC_CLIENT_ID="your-client-id"
export ANTHROPIC_CLIENT_SECRET="your-client-secret"
export ANTHROPIC_REFRESH_TOKEN="your-refresh-token"
```

## Model Reference Format

Use the `provider/model` format with the registry:

```
anthropic:claude-code/sonnet     # Claude Code CLI
anthropic:claude-code/opus       # Claude Code CLI
anthropic/claude-3-5-sonnet-20241022  # Direct Anthropic API
ollama/glm-4.7-flash            # Local Ollama
ollama/llama3.2:1b              # Local Ollama
openrouter/anthropic/claude-sonnet-4.5  # OpenRouter proxy
openrouter/google/gemini-2.0-flash-001  # OpenRouter proxy
```

## Stream Event Types

```go
type StreamEvent struct {
    Type     StreamEventType
    Delta    string           // For StreamEventDelta
    ToolCall *ToolCall        // For StreamEventToolCall
    Usage    *Usage           // For StreamEventDone
    Error    error            // For StreamEventError
}

// Event types
const (
    StreamEventDelta    // Text delta from model
    StreamEventToolCall // Tool call request
    StreamEventDone     // Stream complete (includes usage)
    StreamEventError    // Error occurred
)
```

## Error Handling

```go
events, err := provider.CreateStream(ctx, opts)
if err != nil {
    // Initial request failed (invalid params, auth error, etc.)
    return fmt.Errorf("create stream: %w", err)
}

for event := range events {
    if event.Type == llm.StreamEventError {
        // Stream error (network issue, parse error, etc.)
        return fmt.Errorf("stream error: %w", event.Error)
    }
}
```

## Testing

The library includes comprehensive tests:

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Run integration tests (requires providers)
go test -v ./... -run TestProviders

# Run Ollama compatibility test
go test -v ./... -run TestOllamaModels
```

## Architecture

```
llm/
├── api.go              # Core types: Message, Model, Role, ToolCall
├── provider.go         # Provider interface, StreamEvent, StreamOptions
├── registry.go         # Provider registry, model resolution
├── tool.go             # ToolDefinition
│
├── provider/
│   ├── register.go     # Default registry with env-based config
│   ├── anthropic/      # Claude Code CLI + Direct API
│   ├── ollama/         # Local Ollama integration
│   ├── openrouter/     # OpenRouter proxy (229 models)
│   └── fake/           # Test provider
│
└── cmd/llm/            # CLI demo application
```

## Contributing

Contributions welcome! Please ensure:
- All tests pass: `go test ./...`
- No race conditions: `go test -race ./...`
- Code is formatted: `go fmt ./...`
- Follow existing patterns (see [AGENTS.md](AGENTS.md))

## License

MIT License - see [LICENSE](LICENSE) file for details

## See Also

- [Provider-Specific Documentation](provider/)
- [OpenRouter Models](provider/openrouter/README.md)
- [Developer Guide](AGENTS.md)
