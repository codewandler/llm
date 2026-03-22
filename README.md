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
| Anthropic API | `anthropic` | Direct Anthropic API with API key |
| Claude OAuth | `claude` | OAuth-based Claude access (auto-detects local credentials) |
| OpenAI | `openai` | OpenAI GPT models (GPT-4, GPT-4o, etc.) |
| AWS Bedrock | `bedrock` | AWS Bedrock models (Claude, Llama, etc.) |
| Ollama | `ollama` | Local Ollama models (11 curated defaults) |
| OpenRouter | `openrouter` | 229 tool-enabled models via OpenRouter proxy |
| Aggregate | `aggregate` | Combines multiple providers with failover and aliases |

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
    os.Setenv("OPENAI_KEY", "your-api-key")
    os.Setenv("OPENROUTER_API_KEY", "your-api-key")
    
    ctx := context.Background()
    
    // Create a stream using provider/model format
    events, err := provider.CreateStream(ctx, llm.StreamOptions{
        Model: "ollama/glm-4.7-flash",
        Messages: llm.Messages{
            &llm.UserMsg{Content: "What is the capital of France?"},
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
    "github.com/codewandler/llm/provider/openai"
    "github.com/codewandler/llm/provider/openrouter"
)

// Create empty registry
reg := llm.NewRegistry()

// Register specific providers
reg.Register(ollama.New("http://localhost:11434"))
reg.Register(openai.New("your-api-key"))
reg.Register(openrouter.New("your-api-key"))

// Use the registry
events, err := reg.CreateStream(ctx, llm.StreamOptions{
    Model: "openrouter/anthropic/claude-sonnet-4.5",
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
    },
})
```

## Provider-Specific Usage

### Anthropic API (Direct)

Direct API access with API key:

```go
import "github.com/codewandler/llm/provider/anthropic"

provider := anthropic.New(llm.WithAPIKey("your-api-key"))

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "claude-sonnet-4-6",
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
    },
})
```

### Claude OAuth Provider

OAuth-based access with automatic token refresh. By default, auto-detects credentials from your local Claude installation (`~/.claude/.credentials.json`):

```go
import "github.com/codewandler/llm/provider/anthropic/claude"

// Auto-detect local Claude credentials (default)
provider := claude.New()

// Or with explicit token provider
provider := claude.New(
    claude.WithManagedTokenProvider("my-key", tokenStore, nil),
)

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "claude-sonnet-4-6",
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
    },
})
```

Token management interfaces:
- `TokenStore` - Stores and retrieves tokens (implement for your storage backend)
- `LocalTokenStore` - Reads from `~/.claude/.credentials.json`
- `ManagedTokenProvider` - Wraps a TokenStore with automatic refresh

### OpenAI

Access OpenAI models including GPT-5, GPT-4o, and reasoning models:

```go
import "github.com/codewandler/llm/provider/openai"

provider := openai.New("your-api-key")

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "gpt-4o-mini",
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
    },
})
```

Popular OpenAI models:
- **GPT-5 series** - `gpt-5`, `gpt-5.2`, `gpt-5.2-pro`, `gpt-5-mini`, `gpt-5-nano`
- **GPT-4.1 series** - `gpt-4.1`, `gpt-4.1-mini`, `gpt-4.1-nano`
- **GPT-4o series** - `gpt-4o`, `gpt-4o-mini` (default), `gpt-4-turbo`
- **Reasoning models** - `o3`, `o3-mini`, `o3-pro`, `o1`, `o1-pro`
- **Specialized** - `gpt-5.1-codex`, `gpt-5.2-codex` (code generation)

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
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
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

### AWS Bedrock

Access AWS Bedrock models with AWS credentials:

```go
import "github.com/codewandler/llm/provider/bedrock"

// Uses default AWS credential chain (env vars, ~/.aws/credentials, IAM role)
provider := bedrock.New()

// Or with explicit region
provider := bedrock.New(bedrock.WithRegion("us-east-1"))

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "anthropic.claude-3-5-sonnet-20241022-v2:0",
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
    },
})
```

Supported Bedrock models include Claude, Llama, Mistral, and other models available in your AWS region.

### OpenRouter (Multi-Provider Proxy)

Access 229 tool-enabled models:

```go
import "github.com/codewandler/llm/provider/openrouter"

provider := openrouter.New("your-api-key")

events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "anthropic/claude-sonnet-4.5",
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Hello!"},
    },
})
```

Popular OpenRouter models:
- `anthropic/claude-sonnet-4.5`
- `google/gemini-2.0-flash-001`
- `openai/gpt-4-turbo`
- `meta-llama/llama-3.1-70b-instruct`

See [provider/openrouter/README.md](provider/openrouter/README.md) for full model list.

### Aggregate Provider

Combine multiple providers with failover routing and model aliases:

```go
import "github.com/codewandler/llm/provider/aggregate"

cfg := aggregate.Config{
    Name: "my-aggregate",
    Providers: []aggregate.ProviderInstanceConfig{
        {Name: "primary", Type: "anthropic"},
        {Name: "fallback", Type: "openai"},
    },
    Aliases: map[string][]aggregate.AliasTarget{
        "fast":     {{Provider: "primary", Model: "claude-haiku-4-5"}},
        "default":  {{Provider: "primary", Model: "claude-sonnet-4-6"}},
        "powerful": {{Provider: "primary", Model: "claude-opus-4-6"}},
    },
}

provider, _ := aggregate.New(cfg, factories)

// Use aliases instead of full model names
events, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "default",  // Resolves to claude-sonnet-4-6
    Messages: messages,
})
```

**Standard aliases:**
- `fast` - Fastest/cheapest model (e.g., Haiku)
- `default` - Balanced performance (e.g., Sonnet)
- `powerful` - Most capable model (e.g., Opus)

The aggregate provider tries each target in order until one succeeds, providing automatic failover across accounts or providers.

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
            &llm.AssistantMsg{ToolCalls: []llm.ToolCall{{
                ID: c.ID, Name: c.Name, Arguments: map[string]any{
                    "location": c.Params.Location,
                    "unit": c.Params.Unit,
                },
            }}},
            &llm.ToolCallResult{ToolCallID: c.ID, Output: result},
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
    Messages: llm.Messages{
        &llm.UserMsg{Content: "What's the weather in Paris?"},
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
    Messages: llm.Messages{
        &llm.UserMsg{Content: "What's the weather in Paris?"},
        &llm.AssistantMsg{ToolCalls: []llm.ToolCall{*toolCall}},
        &llm.ToolCallResult{ToolCallID: toolCall.ID, Output: result},
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

## Tool Choice

Control whether and which tools the model should call using `ToolChoice`:

```go
// Let the model decide (default behavior)
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:      "openai/gpt-4o",
    Messages:   messages,
    Tools:      tools,
    ToolChoice: llm.ToolChoiceAuto{},  // or nil for the same behavior
})

// Force the model to call at least one tool
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:      "openai/gpt-4o",
    Messages:   messages,
    Tools:      tools,
    ToolChoice: llm.ToolChoiceRequired{},
})

// Force the model to call a specific tool
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:      "openai/gpt-4o",
    Messages:   messages,
    Tools:      tools,
    ToolChoice: llm.ToolChoiceTool{Name: "get_weather"},
})

// Prevent the model from calling any tools
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:      "openai/gpt-4o",
    Messages:   messages,
    Tools:      tools,
    ToolChoice: llm.ToolChoiceNone{},
})
```

### ToolChoice Types

| Type | Description | OpenAI | Anthropic | Ollama |
|------|-------------|--------|-----------|--------|
| `nil` / `ToolChoiceAuto{}` | Model decides | `"auto"` | `{"type":"auto"}` | (ignored) |
| `ToolChoiceRequired{}` | Must call ≥1 tool | `"required"` | `{"type":"any"}` | (ignored) |
| `ToolChoiceNone{}` | Cannot call tools | `"none"` | (omitted) | (ignored) |
| `ToolChoiceTool{Name:"X"}` | Must call tool "X" | `{"type":"function",...}` | `{"type":"tool","name":"X"}` | (ignored) |

**Note:** Ollama does not support `tool_choice`. All ToolChoice settings are silently ignored and treated as auto behavior.

### Validation

The library validates ToolChoice at request time:
- `ToolChoice` cannot be set without `Tools`
- `ToolChoiceTool{Name: "X"}` must reference an existing tool in `Tools`

```go
opts := llm.StreamOptions{
    Model:      "gpt-4o",
    Messages:   messages,
    Tools:      tools,
    ToolChoice: llm.ToolChoiceTool{Name: "unknown_tool"},
}
err := opts.Validate()  // Error: ToolChoiceTool references unknown tool "unknown_tool"
```

## Reasoning Effort

Control how many reasoning tokens OpenAI models generate before producing a response. Lower reasoning effort means faster responses and fewer tokens used.

```go
// Use low reasoning for faster responses
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:           "openai/gpt-5",
    Messages:        messages,
    ReasoningEffort: llm.ReasoningEffortLow,
})

// Use high reasoning for complex tasks
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:           "openai/o3",
    Messages:        messages,
    ReasoningEffort: llm.ReasoningEffortHigh,
})

// Disable reasoning entirely (GPT-5.1+ only)
stream, _ := provider.CreateStream(ctx, llm.StreamOptions{
    Model:           "openai/gpt-5.1",
    Messages:        messages,
    ReasoningEffort: llm.ReasoningEffortNone,
})
```

### ReasoningEffort Values

| Value | Constant | Description |
|-------|----------|-------------|
| `"none"` | `ReasoningEffortNone` | No reasoning (GPT-5.1+ only) |
| `"minimal"` | `ReasoningEffortMinimal` | Minimal reasoning (pre-5.1 models only) |
| `"low"` | `ReasoningEffortLow` | Low reasoning |
| `"medium"` | `ReasoningEffortMedium` | Medium reasoning (OpenAI API default for pre-5.1) |
| `"high"` | `ReasoningEffortHigh` | High reasoning |
| `"xhigh"` | `ReasoningEffortXHigh` | Maximum reasoning (codex-max+ only) |

### Model-Specific Support

The OpenAI provider maps `ReasoningEffort` values to valid API values per model:

| Model Category | Supported Values | Default | Notes |
|----------------|------------------|---------|-------|
| Non-reasoning (gpt-4o, gpt-4, gpt-3.5) | N/A | N/A | Parameter ignored |
| Pre-5.1 reasoning (gpt-5, o1, o3) | minimal, low, medium, high | medium | `none` not supported |
| gpt-5.1 | none, low, medium, high | none | `minimal` mapped to `low` |
| Pro models (gpt-5-pro, o3-pro) | high only | high | Other values error |
| Codex models (gpt-5.1-codex+) | none, low, medium, high, xhigh | varies | `minimal` mapped to `low` |

### Provider Support

| Provider | Behavior |
|----------|----------|
| **OpenAI** | Model-specific mapping with validation (see above) |
| **OpenRouter** | Passed through if specified, no default |
| **Anthropic** | Ignored (uses different `thinking.budget_tokens` approach) |
| **Ollama** | Ignored |

**Note:** If not specified, the parameter is omitted and the OpenAI API uses its default for the model.

## Prompt Caching

Prompt caching reduces cost and latency on repeated requests by reusing previously
processed input tokens. Behaviour varies by provider — for most use cases, set a
`CacheHint` on `StreamOptions` or on individual messages and the library handles the rest.

### Quick Start

```go
import "github.com/codewandler/llm"

// Enable automatic caching for the entire conversation prefix.
// Works for Anthropic, Bedrock (Claude), and OpenAI (always automatic).
events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "anthropic/claude-sonnet-4-6",
    Messages: llm.Messages{
        &llm.SystemMsg{Content: largeSystemPrompt},
        &llm.UserMsg{Content: "Hello!"},
    },
    CacheHint: &llm.CacheHint{Enabled: true},
})
```

On the **first call**, the provider writes the prompt prefix to cache
(`CacheWriteTokens > 0`). On **subsequent calls within the TTL window** with the same
prefix, the provider reads from cache (`CacheReadTokens > 0`, cost and latency drop
significantly).

Inspect cache usage via the `StreamEventDone` event:

```go
for event := range events {
    if event.Type == llm.StreamEventDone && event.Usage != nil {
        fmt.Printf("cached read:  %d tokens\n", event.Usage.CacheReadTokens)
        fmt.Printf("cached write: %d tokens\n", event.Usage.CacheWriteTokens)
        fmt.Printf("cost:         $%.6f\n",     event.Usage.Cost)
    }
}
```

### Controlling Cache TTL

The default TTL is **5 minutes** (refreshed on each cache hit, at no extra cost).
For workloads with longer processing times, request a **1-hour TTL**:

```go
events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model:     "anthropic/claude-sonnet-4-6",
    Messages:  messages,
    CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"},
})
```

> ⚠️ 1-hour TTL is only available on Claude Haiku 4.5, Sonnet 4.5, and Opus 4.5
> (Anthropic direct and Bedrock). For other models the `TTL: "1h"` hint silently falls
> back to the default 5-minute TTL.

### Fine-Grained Cache Breakpoints (Advanced)

For requests with multiple sections that change at different rates — e.g. static tool
definitions and a growing conversation — attach a `CacheHint` directly to individual
messages. The provider caches everything up to each marked block (up to 4 breakpoints
per request on Anthropic and Bedrock).

```go
events, err := provider.CreateStream(ctx, llm.StreamOptions{
    Model: "anthropic/claude-sonnet-4-6",
    Messages: llm.Messages{
        // Cache the large static system prompt at this breakpoint
        &llm.SystemMsg{
            Content:   largeSystemPrompt,
            CacheHint: &llm.CacheHint{Enabled: true},
        },
        &llm.UserMsg{Content: "Turn 1"},
        &llm.AssistantMsg{Content: "Response 1"},
        // Also cache up to the last user turn
        &llm.UserMsg{
            Content:   "Turn 2",
            CacheHint: &llm.CacheHint{Enabled: true},
        },
    },
})
```

Per-message hints and `StreamOptions.CacheHint` are mutually exclusive: if any message
carries a `CacheHint`, the top-level field is ignored.

### Provider Support Summary

| Provider | Mode | Annotation required | TTL options |
|---|---|---|---|
| **Anthropic** (direct) | Explicit breakpoints | `CacheHint` on messages or `StreamOptions` | `"5m"` (default), `"1h"` (selected models) |
| **Bedrock** (Claude) | Explicit breakpoints | `CacheHint` on messages or `StreamOptions` | `"5m"` (default), `"1h"` (selected models) |
| **OpenAI** | Fully automatic | None (always active) | `"in_memory"` default, `"1h"` via `CacheHint{TTL: "1h"}` |
| **Claude OAuth** | Same as Anthropic | Same as Anthropic | Same as Anthropic |
| **Ollama / OpenRouter** | Not supported | Ignored | — |

### Minimum Token Threshold

Providers only cache prompts above a minimum token count:

| Provider | Minimum |
|---|---|
| Anthropic direct | 1,024 tokens |
| Bedrock (Claude) | 2,048 tokens (varies by model) |
| OpenAI | 1,024 tokens |

Cache hints on smaller prompts are silently ignored — no error is returned. The
`CacheWriteTokens` and `CacheReadTokens` fields in `Usage` will be `0`.

### Pricing

Cache reads are significantly cheaper than regular input tokens:

| Provider | Cache write (relative) | Cache read (relative) |
|---|---|---|
| Anthropic | 1.25× input price | 0.1× input price |
| Bedrock (Claude) | 1.25× input price | 0.1× input price |
| OpenAI | 1× input price (first call) | 0.5× input price |

`Usage.Cost` in the `StreamEventDone` event accounts for cache read and write pricing
automatically.

## Multi-Turn Conversations

Build conversations by appending messages:

```go
messages := llm.Messages{
    &llm.UserMsg{Content: "Hello!"},
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
messages = append(messages, &llm.AssistantMsg{Content: response})

// Second turn
messages = append(messages, &llm.UserMsg{Content: "Tell me more about that"})

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
    Messages: llm.Messages{
        &llm.UserMsg{Content: "Write a very long essay"},
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
# Anthropic (API key)
export ANTHROPIC_API_KEY="your-api-key"

# OpenAI
export OPENAI_KEY="your-api-key"

# OpenRouter
export OPENROUTER_API_KEY="your-api-key"

# Ollama (optional, defaults to http://localhost:11434)
export OLLAMA_BASE_URL="http://localhost:11434"

# AWS Bedrock (uses standard AWS credential chain)
export AWS_REGION="us-east-1"
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
```

**Note:** The Claude OAuth provider auto-detects credentials from `~/.claude/.credentials.json` (created by Claude Code CLI).

## Model Reference Format

Use the `provider/model` format with the registry:

```
anthropic/claude-sonnet-4-6           # Direct Anthropic API
claude/claude-sonnet-4-6              # Claude OAuth provider
openai/gpt-4o                         # OpenAI
openai/gpt-4o-mini                    # OpenAI
bedrock/anthropic.claude-3-5-sonnet   # AWS Bedrock
ollama/glm-4.7-flash                  # Local Ollama
ollama/llama3.2:1b                    # Local Ollama
openrouter/anthropic/claude-sonnet-4.5  # OpenRouter proxy
openrouter/google/gemini-2.0-flash-001  # OpenRouter proxy
```

## Stream Event Types

```go
type StreamEvent struct {
    Type     StreamEventType
    Start    *StreamStart     // For StreamEventStart
    Delta    string           // For StreamEventDelta
    ToolCall *ToolCall        // For StreamEventToolCall
    Usage    *Usage           // For StreamEventDone
    Error    error            // For StreamEventError
}

// Event types
const (
    StreamEventStart    // Stream metadata (first event)
    StreamEventDelta    // Text delta from model
    StreamEventToolCall // Tool call request
    StreamEventDone     // Stream complete (includes usage)
    StreamEventError    // Error occurred
)
```

### Stream Start Metadata

The `StreamEventStart` event is emitted first and contains request metadata:

```go
type StreamStart struct {
    RequestID        string        // Provider request ID (e.g., "msg_01XFDUDYJgAACzvnptvVoYEL")
    RequestedModel   string        // Model requested by caller
    ResolvedModel    string        // Model after alias resolution
    ProviderModel    string        // Actual model from API response
    TimeToFirstToken time.Duration // Time until first content token
}
```

**Usage:**

```go
for event := range stream {
    switch event.Type {
    case llm.StreamEventStart:
        fmt.Printf("Request ID: %s\n", event.Start.RequestID)
        fmt.Printf("Model: %s -> %s\n", event.Start.RequestedModel, event.Start.ProviderModel)
    case llm.StreamEventDelta:
        fmt.Print(event.Delta)
    }
}
```

### Usage Information

The `Usage` struct provides token counts and detailed breakdown:

```go
type Usage struct {
    InputTokens     int     // Total input tokens (uncached + cache-read + cache-write)
    OutputTokens    int     // Completion tokens
    TotalTokens     int     // InputTokens + OutputTokens
    Cost            float64 // Cost in USD (Anthropic, OpenRouter)

    // Detailed breakdown (provider-specific, may be zero)
    CacheReadTokens  int // Input tokens served from an existing cache entry
    CacheWriteTokens int // Input tokens written to a new cache entry
    ReasoningTokens  int // Tokens used for model reasoning
}
```

**Usage in streaming:**

```go
for event := range stream {
    if event.Type == llm.StreamEventDone && event.Usage != nil {
        fmt.Printf("Tokens: %d in, %d out\n",
            event.Usage.InputTokens, event.Usage.OutputTokens)

        if event.Usage.CacheReadTokens > 0 {
            fmt.Printf("Cache hit: %d tokens\n", event.Usage.CacheReadTokens)
        }
        if event.Usage.ReasoningTokens > 0 {
            fmt.Printf("Reasoning: %d tokens\n", event.Usage.ReasoningTokens)
        }
    }
}
```

| Field | Description | Providers |
|-------|-------------|-----------|
| `InputTokens` | Total input tokens processed (uncached + cache-read + cache-write) | All |
| `OutputTokens` | Completion tokens | All |
| `TotalTokens` | InputTokens + OutputTokens | All |
| `Cost` | Cost in USD | Anthropic (calculated), OpenRouter |
| `CacheReadTokens` | Input tokens served from an existing cache entry | Anthropic, Bedrock, OpenAI, OpenRouter |
| `CacheWriteTokens` | Input tokens written to a new cache entry | Anthropic, Bedrock |
| `ReasoningTokens` | Tokens used for reasoning | OpenAI, OpenRouter (reasoning models) |

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
│   ├── aggregate/      # Multi-provider aggregation with failover
│   ├── anthropic/      # Direct Anthropic API
│   │   └── claude/     # OAuth-based Claude provider
│   ├── bedrock/        # AWS Bedrock
│   ├── openai/         # OpenAI API
│   ├── ollama/         # Local Ollama integration
│   ├── openrouter/     # OpenRouter proxy (229 models)
│   └── fake/           # Test provider
│
└── cmd/llmcli/         # CLI tool for testing and OAuth management
```

## CLI Tool

The `llmcli` tool provides quick testing and OAuth credential management:

```bash
# Check auth status (uses ~/.claude/.credentials.json)
go run ./cmd/llmcli auth status

# Quick inference
go run ./cmd/llmcli infer "Hello, how are you?"

# Verbose output with model info, tokens, cost, and timing
go run ./cmd/llmcli infer -v -m default "Explain Go channels"

# Model aliases: fast (haiku), default (sonnet), powerful (opus)
go run ./cmd/llmcli infer -m powerful "Complex analysis task"
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
