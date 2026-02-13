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

All providers support tool/function calling:

```go
// Define tools
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

// Create stream with tools
events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model:    "ollama/glm-4.7-flash",
    Messages: []llm.Message{
        {Role: llm.RoleUser, Content: "What's the weather in Paris?"},
    },
    Tools: tools,
})

// Process tool calls
for event := range events {
    if event.Type == llm.StreamEventToolCall {
        fmt.Printf("Tool: %s\n", event.ToolCall.Name)
        fmt.Printf("Args: %+v\n", event.ToolCall.Arguments)
        
        // Execute tool and send result back
        result := executeWeatherTool(event.ToolCall.Arguments)
        
        // Continue conversation with tool result
        events2, _ := provider.CreateStream(ctx, llm.StreamOptions{
            Model: "ollama/glm-4.7-flash",
            Messages: []llm.Message{
                {Role: llm.RoleUser, Content: "What's the weather in Paris?"},
                {
                    Role: llm.RoleAssistant,
                    ToolCalls: []llm.ToolCall{*event.ToolCall},
                },
                {
                    Role:       llm.RoleTool,
                    Content:    result,
                    ToolCallID: event.ToolCall.ID,
                },
            },
            Tools: tools,
        })
    }
}
```

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
