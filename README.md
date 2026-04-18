# LLM Provider Abstraction Library

A unified Go library for interacting with multiple LLM providers through a consistent interface. Supports streaming responses, tool calling, reasoning, prompt caching, and zero-config multi-provider setup.

## Features

- **Unified backend interface** — real backends implement `llm.Provider`
- **Service runtime** — `llm.Service` resolves model strings, applies intent aliases, and performs fallback
- **Streaming support** — channel-based streaming with structured event envelopes
- **Tool calling** — consistent tool/function calling across providers
- **Reasoning support** — Anthropic, OpenAI reasoning models, Bedrock-compatible providers
- **Prompt caching** — transparent cache control where supported
- **Zero-config setup** — `provider/auto` builds a ready-to-use `*llm.Service`
- **Model catalog integration** — catalog-backed model resolution, aliases, and preference-aware routing

## Supported Providers

| Provider | Name | Description |
|----------|------|-------------|
| Anthropic API | `anthropic` | Direct Anthropic API with API key |
| Claude OAuth | `claude` | OAuth-based Claude access |
| OpenAI | `openai` | OpenAI GPT models |
| AWS Bedrock | `bedrock` | AWS Bedrock models |
| MiniMax | `minimax` | MiniMax models via Anthropic-compatible API |
| Ollama | `ollama` | Local Ollama models |
| OpenRouter | `openrouter` | OpenRouter proxy |
| Docker Model Runner | `dockermr` | Local Docker model runtime |

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

    svc, err := auto.New(ctx)
    if err != nil {
        panic(err)
    }

    stream, err := svc.CreateStream(ctx, llm.Request{
        Model: "default",
        Messages: llm.Messages{
            llm.User("What is the capital of France?"),
        },
    })
    if err != nil {
        panic(err)
    }

    for ev := range stream {
        switch ev.Type {
        case llm.StreamEventDelta:
            if d, ok := ev.Data.(*llm.DeltaEvent); ok {
                fmt.Print(d.Text())
            }
        case llm.StreamEventCompleted:
            fmt.Println()
        case llm.StreamEventError:
            fmt.Printf("error: %v
", ev.Data)
        }
    }
}
```

## Service-first API

`llm.New(opts...)` builds the main orchestration runtime.

```go
svc, err := llm.New(
    llm.WithAutoDetect(),
)
```

Or register providers explicitly:

```go
svc, err := llm.New(
    llm.WithProvider(openai.New(llm.APIKeyFromEnv("OPENAI_API_KEY"))),
    llm.WithProviderNamed("work", anthropic.New(llm.APIKeyFromEnv("ANTHROPIC_API_KEY"))),
    llm.WithIntentAlias("fast", llm.IntentSelector{Model: "openai/gpt-4o-mini"}),
)
```

### Model reference styles

Recommended reference ladder:

- `fast`, `default`, `powerful` — intent aliases
- `provider/model` — preferred explicit form, e.g. `openai/gpt-4o`
- `instance/provider/model` — exact instance targeting, e.g. `work/anthropic/claude-sonnet-4-6`
- bare IDs only when convenient and unambiguous

## `provider/auto`

`provider/auto` is now a convenience layer over `llm.New(...)`.

```go
import "github.com/codewandler/llm/provider/auto"

svc, err := auto.New(ctx)

svc, err := auto.New(ctx,
    auto.WithAnthropic(),
    auto.WithOpenAI(),
    auto.WithBedrock(),
    auto.WithClaudeLocal(),
)

svc, err := auto.New(ctx,
    auto.WithOpenAI(),
    auto.WithGlobalAlias("review", "openai/gpt-4o"),
)
```

## Direct provider usage

Real backends still implement `llm.Provider` directly:

```go
p := openai.New(llm.APIKeyFromEnv("OPENAI_API_KEY"))
stream, err := p.CreateStream(ctx, llm.Request{
    Model: "gpt-4o",
    Messages: llm.Messages{llm.User("Hello")},
})
```

## Streams and events

Streams are `llm.Stream` (`<-chan llm.Envelope`). Common event types include:

- `StreamEventStarted`
- `StreamEventTokenEstimate`
- `StreamEventDelta`
- `StreamEventToolCall`
- `StreamEventUsageUpdated`
- `StreamEventCompleted`
- `StreamEventError`
- `StreamEventRequest`

Use `llm.NewEventProcessor(ctx, stream)` for high-level consumption.

## Tool calling

Type-safe tools are built with `github.com/codewandler/llm/tool`.

```go
spec := tool.NewSpec[GetWeatherParams]("get_weather", "Get current weather")

result := llm.NewEventProcessor(ctx, stream).
    HandleTool(tool.Handle(spec, func(ctx context.Context, p GetWeatherParams) (*GetWeatherResult, error) {
        return doWeather(p.Location, p.Unit)
    })).
    Result()
```

## Architecture

```text
llm/
├── service.go              # llm.Service, llm.New(...), service options
├── request.go              # Request, validation, effort, thinking, api type
├── request_builder.go      # Buildable + RequestBuilder
├── event.go                # Envelope, event types, stream model
├── event_processor.go      # Stream consumption helper
├── msg/                    # Canonical message model
├── tool/                   # Tool definitions and typed dispatch
├── usage/                  # Pricing and usage tracking
├── tokencount/             # Token estimation
├── internal/modelcatalog/  # Built-in catalog loading + canonicalization
├── internal/modelview/     # Catalog projections and visible-model views
├── internal/providerregistry/ # Provider detect/build registry
└── provider/
    ├── auto/               # Convenience service builder
    ├── anthropic/
    ├── bedrock/
    ├── codex/
    ├── dockermr/
    ├── fake/
    ├── minimax/
    ├── ollama/
    ├── openai/
    ├── openrouter/
    └── router/             # legacy/transitional package, not the preferred runtime path
```

## CLI

```bash
go run ./cmd/llmcli infer "Hello"
go run ./cmd/llmcli infer -v -m default "Explain Go channels"
```

## Contributing

```bash
go test ./...
go fmt ./...
go vet ./...
```
