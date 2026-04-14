# Plan: Docker Model Runner Provider

**Date**: 2025-07  
**Ref design**: This document is also the design — no separate design doc needed.

---

## Research Summary

### What is Docker Model Runner (DMR)?

Docker Model Runner (DMR) is Docker Desktop 4.40+'s built-in LLM inference engine, released April 2025. It uses **llama.cpp** as the default backend (vLLM and Diffusers also available on Linux/NVIDIA) and exposes an **OpenAI-compatible API** — making it a near-drop-in replacement for Ollama or the OpenAI API for local inference.

### API Surface

| Endpoint | Purpose |
|---|---|
| `POST /engines/{engine}/v1/chat/completions` | Chat (OpenAI-format SSE streaming) |
| `POST /engines/{engine}/v1/completions` | Legacy completions |
| `POST /engines/{engine}/v1/embeddings` | Embeddings |
| `GET /engines/{engine}/v1/models` | OpenAI-format model listing |
| `GET /models` | Docker-style model listing |

- `{engine}` defaults to `llama.cpp` and can be omitted: `/engines/v1/chat/completions`
- **No API key required**
- SSE format is byte-for-byte identical to OpenAI Chat Completions

### Endpoint Addresses

| Context | URL |
|---|---|
| Docker Desktop host (TCP enabled) | `http://localhost:12434` |
| Inside Docker Desktop containers | `http://model-runner.docker.internal` |
| Docker CE (inside containers) | `http://172.17.0.1:12434` |

### Available Models (`ai/` namespace on Docker Hub)

Representative selection (all in GGUF format for llama.cpp):
- `ai/smollm2` — 135M/360M (tiny, good for tests)
- `ai/qwen2.5` — 0.5B–7B
- `ai/qwen3`, `ai/qwen3-coder`
- `ai/llama3.2`, `ai/llama3.3`
- `ai/phi4`, `ai/phi4-mini`
- `ai/gemma3`, `ai/gemma4`
- `ai/mistral-small3.2`, `ai/mistral-nemo`
- `ai/deepseek-r1`
- `ai/glm-4.7-flash`
- `ai/granite4.0-nano`
- `ai/functiongemma`

### DMR vs Ollama

| Dimension | Ollama | Docker Model Runner |
|---|---|---|
| **API format** | Custom Ollama (`/api/chat`) | OpenAI-compatible (`/engines/v1/chat/completions`) |
| **Model registry** | ollama.com — `model:tag` | Docker Hub OCI artifacts — `ai/model:tag` |
| **Model format** | GGUF (converted internally) | GGUF via llama.cpp, Safetensors via vLLM |
| **GPU** | Broad: CUDA, ROCm, Metal | Apple Silicon, NVIDIA (CUDA), AMD (Vulkan/ROCm) |
| **Platform** | All platforms (standalone daemon) | Docker Desktop 4.40+ or Docker CE with plugin |
| **Integration** | Separate process | Native Docker Desktop feature |
| **Auth** | None | None |
| **Model IDs** | Short names (`qwen2.5:0.5b`) | Namespaced (`ai/qwen2.5:0.5B-F16`) |
| **Cost** | Free (local) | Free (local) |
| **Detection** | Check `localhost:11434` | Check `localhost:12434` |
| **Code reuse opportunity** | Unique API | OpenAI-compatible → reuse `internal/sse` |

### Key implementation insight

Because DMR uses OpenAI-compatible SSE, we can reuse `internal/sse` (already used by the OpenAI provider) for stream parsing. The new provider is essentially the OpenAI Chat Completions wire format with:
- A different base URL and path (`/engines/llama.cpp/v1/` prefix)
- No API key header
- `ai/model:tag` model IDs
- Local inference → no cost calculation

---

## Scope

**In scope:**
- New `provider/dockermr` package
- `ProviderNameDockerMR` constant in `errors.go`
- Auto-detection in `provider/auto`
- `WithDockerModelRunner()` option in `provider/auto`
- Table-driven tests

**Out of scope:**
- vLLM or Diffusers backends (llama.cpp default only)
- Docker socket (`/var/run/docker.sock`) transport
- `docker model pull` / model management commands
- Embeddings support (future)
- Image generation (Diffusers, future)

---

## Task List

### Task 1 — Add provider name constant

**Files modified**: `errors.go`  
**Estimated time**: 2 minutes

Add `ProviderNameDockerMR = "dockermr"` to the provider name constants block.

```go
// In errors.go — add after ProviderNameOllama:
ProviderNameDockerMR   = "dockermr"
```

**Verification:**
```bash
go build ./...
```

---

### Task 2 — Create `provider/dockermr/models.go`

**Files created**: `provider/dockermr/models.go`  
**Estimated time**: 4 minutes

Define curated model constants and the static `Models()` list.

```go
package dockermr

import "github.com/codewandler/llm"

// Known model IDs in Docker Hub's ai/ namespace.
// Model IDs use the format ai/<name>:<tag>.
// Tags follow the pattern <size>-<quantization> (e.g. 7B-Q4_K_M).
// "latest" resolves to a sensible default for each model family.
const (
    ModelSmoLLM2         = "ai/smollm2"            // 360M Q4_K_M (default)
    ModelSmoLLM2Tiny     = "ai/smollm2:135M-Q4_K_M"
    ModelQwen25          = "ai/qwen2.5"             // 7B Q4_K_M
    ModelQwen25Small     = "ai/qwen2.5:0.5B-F16"
    ModelQwen3           = "ai/qwen3"
    ModelQwen3Coder      = "ai/qwen3-coder"
    ModelLlama32         = "ai/llama3.2"
    ModelLlama33         = "ai/llama3.3"
    ModelPhi4Mini        = "ai/phi4-mini"
    ModelPhi4            = "ai/phi4"
    ModelGemma3          = "ai/gemma3"
    ModelGemma4          = "ai/gemma4"
    ModelDeepSeekR1      = "ai/deepseek-r1"
    ModelMistralSmall    = "ai/mistral-small3.2"
    ModelGLM47Flash      = "ai/glm-4.7-flash"
    ModelGranite4Nano    = "ai/granite4.0-nano"
    ModelFunctionGemma   = "ai/functiongemma"

    ModelDefault = ModelSmoLLM2
)

// curatedModels is the static list returned by Provider.Models().
// FetchModels() returns the live list of locally pulled models.
var curatedModels = []llm.Model{
    {ID: ModelSmoLLM2,       Name: "SmolLM2 360M",        Provider: llm.ProviderNameDockerMR},
    {ID: ModelSmoLLM2Tiny,   Name: "SmolLM2 135M",        Provider: llm.ProviderNameDockerMR},
    {ID: ModelQwen25Small,   Name: "Qwen2.5 0.5B",        Provider: llm.ProviderNameDockerMR},
    {ID: ModelQwen25,        Name: "Qwen2.5 7B",          Provider: llm.ProviderNameDockerMR},
    {ID: ModelQwen3,         Name: "Qwen3",               Provider: llm.ProviderNameDockerMR},
    {ID: ModelQwen3Coder,    Name: "Qwen3 Coder",         Provider: llm.ProviderNameDockerMR},
    {ID: ModelLlama32,       Name: "Llama 3.2",           Provider: llm.ProviderNameDockerMR},
    {ID: ModelLlama33,       Name: "Llama 3.3",           Provider: llm.ProviderNameDockerMR},
    {ID: ModelPhi4Mini,      Name: "Phi-4 Mini",          Provider: llm.ProviderNameDockerMR},
    {ID: ModelPhi4,          Name: "Phi-4",               Provider: llm.ProviderNameDockerMR},
    {ID: ModelGemma3,        Name: "Gemma 3",             Provider: llm.ProviderNameDockerMR},
    {ID: ModelGemma4,        Name: "Gemma 4",             Provider: llm.ProviderNameDockerMR},
    {ID: ModelDeepSeekR1,    Name: "DeepSeek R1",         Provider: llm.ProviderNameDockerMR},
    {ID: ModelMistralSmall,  Name: "Mistral Small 3.2",   Provider: llm.ProviderNameDockerMR},
    {ID: ModelGLM47Flash,    Name: "GLM-4.7 Flash",       Provider: llm.ProviderNameDockerMR},
    {ID: ModelGranite4Nano,  Name: "Granite 4.0 Nano",    Provider: llm.ProviderNameDockerMR},
    {ID: ModelFunctionGemma, Name: "FunctionGemma",       Provider: llm.ProviderNameDockerMR},
}
```

**Verification:**
```bash
go build ./provider/dockermr/...
```

---

### Task 3 — Create `provider/dockermr/dockermr.go` (core provider)

**Files created**: `provider/dockermr/dockermr.go`  
**Estimated time**: 8 minutes

```go
// Package dockermr implements the Docker Model Runner (DMR) provider.
//
// Docker Model Runner is built into Docker Desktop 4.40+ and Docker Engine
// (via the docker-model-plugin). It exposes an OpenAI-compatible API for
// running locally-pulled models from Docker Hub's ai/ namespace.
//
// Endpoints (no API key required):
//   - Host (Docker Desktop TCP mode): http://localhost:12434/engines/llama.cpp/v1
//   - Container (Docker Desktop):     http://model-runner.docker.internal/engines/llama.cpp/v1
//   - Container (Docker CE):          http://172.17.0.1:12434/engines/llama.cpp/v1
package dockermr

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/internal/sse"
    "github.com/codewandler/llm/usage"
)

const (
    // DefaultBaseURL is the host-side TCP endpoint (Docker Desktop with TCP enabled,
    // or Docker CE on the loopback interface).
    DefaultBaseURL = "http://localhost:12434"

    // ContainerBaseURL is accessible from inside Docker Desktop containers.
    ContainerBaseURL = "http://model-runner.docker.internal"

    // dockerCEContainerBaseURL is the gateway address usable inside Docker CE containers.
    dockerCEContainerBaseURL = "http://172.17.0.1:12434"

    // defaultEngine is the inference backend. llama.cpp is the only engine
    // supported on all platforms. Future work: support "vllm".
    defaultEngine = "llama.cpp"
)

// Provider implements the Docker Model Runner LLM backend.
type Provider struct {
    opts         *llm.Options
    defaultModel string
    engine       string
    client       *http.Client
}

// DefaultOptions returns the default options for the DMR provider.
// No API key is required; the base URL defaults to localhost:12434.
func DefaultOptions() []llm.Option {
    return []llm.Option{
        llm.WithBaseURL(DefaultBaseURL),
    }
}

// New creates a new Docker Model Runner provider.
// Options are applied on top of DefaultOptions().
func New(opts ...llm.Option) *Provider {
    allOpts := append(DefaultOptions(), opts...)
    cfg := llm.Apply(allOpts...)
    client := cfg.HTTPClient
    if client == nil {
        client = llm.DefaultHttpClient()
    }
    return &Provider{
        opts:         cfg,
        defaultModel: ModelDefault,
        engine:       defaultEngine,
        client:       client,
    }
}

// WithEngine returns a copy of the provider using the specified inference engine.
// Currently only "llama.cpp" is supported on all platforms.
// "vllm" requires Linux with NVIDIA GPU.
func (p *Provider) WithEngine(engine string) *Provider {
    clone := *p
    clone.engine = engine
    return &clone
}

// WithDefaultModel sets the default model to use when none is specified.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
    clone := *p
    clone.defaultModel = modelID
    return &clone
}

func (p *Provider) Name() string { return llm.ProviderNameDockerMR }

func (p *Provider) CostCalculator() usage.CostCalculator {
    // DMR is local inference; no cost information is available.
    return usage.CostCalculatorFunc(func(_, _ string, _ usage.TokenItems) (usage.Cost, bool) {
        return usage.Cost{}, false
    })
}

// Models returns the curated list of publicly available ai/ namespace models.
// Call FetchModels to get the live list of locally pulled models instead.
func (p *Provider) Models() []llm.Model {
    return curatedModels
}

// FetchModels queries the DMR endpoint for locally available (pulled) models.
func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
    url := fmt.Sprintf("%s/engines/%s/v1/models", p.opts.BaseURL, p.engine)
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, fmt.Errorf("dockermr list models: %w", err)
    }

    resp, err := p.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("dockermr list models: %w", err)
    }
    defer resp.Body.Close() //nolint:errcheck

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, llm.NewErrAPIError(llm.ProviderNameDockerMR, resp.StatusCode, string(body))
    }

    // OpenAI-format model list: {"data": [{"id": "ai/smollm2", ...}]}
    var result struct {
        Data []struct {
            ID string `json:"id"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("dockermr decode models: %w", err)
    }

    models := make([]llm.Model, len(result.Data))
    for i, m := range result.Data {
        models[i] = llm.Model{
            ID:       m.ID,
            Name:     m.ID,
            Provider: llm.ProviderNameDockerMR,
        }
    }
    return models, nil
}

// CreateStream sends a chat completions request to DMR and streams the response.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
    opts, err := src.BuildRequest(ctx)
    if err != nil {
        return nil, llm.NewErrBuildRequest(llm.ProviderNameDockerMR, err)
    }
    if err := opts.Validate(); err != nil {
        return nil, llm.NewErrBuildRequest(llm.ProviderNameDockerMR, err)
    }

    body, err := buildRequest(opts)
    if err != nil {
        return nil, llm.NewErrBuildRequest(llm.ProviderNameDockerMR, err)
    }

    url := fmt.Sprintf("%s/engines/%s/v1/chat/completions", p.opts.BaseURL, p.engine)
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    if err != nil {
        return nil, llm.NewErrBuildRequest(llm.ProviderNameDockerMR, err)
    }
    req.Header.Set("Content-Type", "application/json")
    // No Authorization header — DMR requires no API key.

    startTime := time.Now()
    resp, err := p.client.Do(req)
    if err != nil {
        return nil, llm.NewErrRequestFailed(llm.ProviderNameDockerMR, err)
    }
    if resp.StatusCode != http.StatusOK {
        defer resp.Body.Close() //nolint:errcheck
        errBody, _ := io.ReadAll(resp.Body)
        return nil, llm.NewErrAPIError(llm.ProviderNameDockerMR, resp.StatusCode, string(errBody))
    }

    pub, ch := llm.NewEventPublisher()
    go parseStream(ctx, resp.Body, pub, streamMeta{
        requestedModel: opts.Model,
        startTime:      startTime,
    })
    return ch, nil
}

// engineURL returns the base path for the configured engine.
func (p *Provider) engineURL() string {
    return fmt.Sprintf("%s/engines/%s/v1", p.opts.BaseURL, p.engine)
}
```

**Notes on `buildRequest`**: This lives in a separate `request.go` file (Task 4). It builds the OpenAI-compatible request body (identical to the openai provider's `ccBuildRequest` logic, with tool calls, messages, streaming flag).

**Verification:**
```bash
go build ./provider/dockermr/...
```

---

### Task 4 — Create `provider/dockermr/request.go` and `provider/dockermr/stream.go`

**Files created**: `provider/dockermr/request.go`, `provider/dockermr/stream.go`  
**Estimated time**: 10 minutes

`request.go` — build the OpenAI-format JSON body:

```go
package dockermr

import (
    "encoding/json"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/msg"
    "github.com/codewandler/llm/sortmap"
    "github.com/codewandler/llm/tool"
)

type chatRequest struct {
    Model       string           `json:"model"`
    Messages    []chatMessage    `json:"messages"`
    Tools       []chatTool       `json:"tools,omitempty"`
    Stream      bool             `json:"stream"`
    MaxTokens   int              `json:"max_tokens,omitempty"`
    Temperature float64          `json:"temperature,omitempty"`
    TopP        float64          `json:"top_p,omitempty"`
}

type chatMessage struct {
    Role       string          `json:"role"`
    Content    any             `json:"content"`            // string or []contentPart
    ToolCalls  []chatToolCall  `json:"tool_calls,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
    Name       string          `json:"name,omitempty"`
}

type chatToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"`
    Function functionCall `json:"function"`
}

type functionCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
}

type chatTool struct {
    Type     string          `json:"type"`
    Function functionDef     `json:"function"`
}

type functionDef struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Parameters  any    `json:"parameters"`
}

func buildRequest(opts llm.Request) ([]byte, error) {
    r := chatRequest{
        Model:  opts.Model,
        Stream: true,
    }
    if opts.MaxTokens > 0 {
        r.MaxTokens = opts.MaxTokens
    }
    if opts.Temperature > 0 {
        r.Temperature = opts.Temperature
    }
    if opts.TopP > 0 {
        r.TopP = opts.TopP
    }

    for _, t := range opts.Tools {
        r.Tools = append(r.Tools, chatTool{
            Type: "function",
            Function: functionDef{
                Name:        t.Name,
                Description: t.Description,
                Parameters:  sortmap.NewSortedMap(t.Parameters),
            },
        })
    }

    for _, m := range opts.Messages {
        switch m.Role {
        case msg.RoleSystem:
            r.Messages = append(r.Messages, chatMessage{Role: "system", Content: m.Text()})
        case msg.RoleUser:
            r.Messages = append(r.Messages, chatMessage{Role: "user", Content: m.Text()})
        case msg.RoleAssistant:
            cm := chatMessage{Role: "assistant", Content: m.Text()}
            for _, tc := range m.ToolCalls() {
                argsJSON, _ := json.Marshal(tc.Args)
                cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
                    ID:   tc.ID,
                    Type: "function",
                    Function: functionCall{
                        Name:      tc.Name,
                        Arguments: string(argsJSON),
                    },
                })
            }
            r.Messages = append(r.Messages, cm)
        case msg.RoleTool:
            for _, tr := range m.ToolResults() {
                r.Messages = append(r.Messages, chatMessage{
                    Role:       "tool",
                    Content:    tr.ToolOutput,
                    ToolCallID: tr.ToolCallID,
                })
            }
        }
    }

    return json.Marshal(r)
}
```

`stream.go` — parse the OpenAI-compatible SSE stream (same wire format as OpenAI Chat Completions):

```go
package dockermr

import (
    "context"
    "encoding/json"
    "io"
    "strings"
    "time"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/internal/sse"
    "github.com/codewandler/llm/tool"
    "github.com/codewandler/llm/usage"
)

type streamMeta struct {
    requestedModel string
    startTime      time.Time
}

// OpenAI-compatible stream chunk structure
type streamChunk struct {
    ID      string `json:"id"`
    Model   string `json:"model"`
    Choices []struct {
        Delta struct {
            Role      string `json:"role"`
            Content   string `json:"content"`
            ToolCalls []struct {
                Index    int    `json:"index"`
                ID       string `json:"id"`
                Type     string `json:"type"`
                Function struct {
                    Name      string `json:"name"`
                    Arguments string `json:"arguments"`
                } `json:"function"`
            } `json:"tool_calls"`
        } `json:"delta"`
        FinishReason *string `json:"finish_reason"`
    } `json:"choices"`
    Usage *struct {
        PromptTokens     int `json:"prompt_tokens"`
        CompletionTokens int `json:"completion_tokens"`
    } `json:"usage"`
}

// toolBuffer accumulates streaming tool call fragments.
type toolBuffer struct {
    id       string
    name     string
    argsJSON strings.Builder
}

func parseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta streamMeta) {
    defer pub.Close()
    defer body.Close() //nolint:errcheck

    scanner := sse.NewScanner(body)
    tools := make(map[int]*toolBuffer)
    startEmitted := false
    resolvedModel := meta.requestedModel

    for scanner.Scan() {
        select {
        case <-ctx.Done():
            pub.Error(llm.NewErrContextCancelled(llm.ProviderNameDockerMR, ctx.Err()))
            return
        default:
        }

        data := scanner.Data()
        if data == "[DONE]" {
            // Flush any accumulated tool calls
            for _, tb := range tools {
                var args map[string]any
                _ = json.Unmarshal([]byte(tb.argsJSON.String()), &args)
                pub.ToolCall(tool.NewToolCall(tb.id, tb.name, args))
            }
            return
        }

        var chunk streamChunk
        if err := json.Unmarshal([]byte(data), &chunk); err != nil {
            pub.Error(llm.NewErrStreamDecode(llm.ProviderNameDockerMR, err))
            return
        }

        if chunk.Model != "" {
            resolvedModel = chunk.Model
        }

        if !startEmitted {
            startEmitted = true
            pub.Started(llm.StreamStartedEvent{})
        }

        for _, choice := range chunk.Choices {
            if choice.Delta.Content != "" {
                pub.Delta(llm.TextDelta(choice.Delta.Content))
            }

            // Accumulate streaming tool call fragments
            for _, tc := range choice.Delta.ToolCalls {
                tb, ok := tools[tc.Index]
                if !ok {
                    tb = &toolBuffer{}
                    tools[tc.Index] = tb
                }
                if tc.ID != "" {
                    tb.id = tc.ID
                }
                if tc.Function.Name != "" {
                    tb.name = tc.Function.Name
                }
                tb.argsJSON.WriteString(tc.Function.Arguments)
            }

            if choice.FinishReason != nil {
                stopReason := finishReasonToStop(*choice.FinishReason, len(tools) > 0)
                var inputTok, outputTok int
                if chunk.Usage != nil {
                    inputTok = chunk.Usage.PromptTokens
                    outputTok = chunk.Usage.CompletionTokens
                }
                tokens := usage.TokenItems{
                    {Kind: usage.KindInput, Count: inputTok},
                    {Kind: usage.KindOutput, Count: outputTok},
                }.NonZero()
                pub.UsageRecord(usage.Record{
                    Dims:       usage.Dims{Provider: llm.ProviderNameDockerMR, Model: resolvedModel},
                    Tokens:     tokens,
                    RecordedAt: time.Now(),
                })
                pub.Completed(llm.CompletedEvent{StopReason: stopReason})
                return
            }
        }
    }

    if err := scanner.Err(); err != nil {
        pub.Error(llm.NewErrStreamRead(llm.ProviderNameDockerMR, err))
    }
}

func finishReasonToStop(reason string, hasTools bool) llm.StopReason {
    switch reason {
    case "stop":
        if hasTools {
            return llm.StopReasonToolUse
        }
        return llm.StopReasonEndTurn
    case "length":
        return llm.StopReasonMaxTokens
    case "tool_calls":
        return llm.StopReasonToolUse
    default:
        return llm.StopReasonEndTurn
    }
}
```

**Note on `sse.NewScanner`**: Check how `internal/sse` is used in the OpenAI provider and adapt. If the scanner API differs, adjust accordingly.

**Verification:**
```bash
go build ./provider/dockermr/...
go vet ./provider/dockermr/...
```

---

### Task 5 — Create `provider/dockermr/token_counter.go`

**Files created**: `provider/dockermr/token_counter.go`  
**Estimated time**: 3 minutes

DMR uses the same models as Ollama (GGUF, llama.cpp). Use a simple character-based estimate since we have no tiktoken access. Mirror the Ollama token counter pattern.

```go
package dockermr

import (
    "context"

    "github.com/codewandler/llm/tokencount"
)

// CountTokens provides a rough token estimate for DMR models.
// Since DMR uses llama.cpp with GGUF models (same as Ollama), we use
// the same character-based approximation: ~4 chars per token.
func (p *Provider) CountTokens(ctx context.Context, req tokencount.TokenCountRequest) (tokencount.TokenCountResult, error) {
    return tokencount.EstimateFromMessages(req.Messages, req.Tools), nil
}
```

**Verification:**
```bash
go build ./provider/dockermr/...
```

---

### Task 6 — Write tests: `provider/dockermr/dockermr_test.go`

**Files created**: `provider/dockermr/dockermr_test.go`  
**Estimated time**: 12 minutes

Table-driven tests for:
1. `New()` — provider is correctly constructed
2. `buildRequest()` — verifies JSON shape for messages, tools, streaming flag
3. `parseStream()` — using synthetic SSE fixtures (no network required)
4. `FetchModels()` — using `httptest.Server`
5. `finishReasonToStop()` — all cases

```go
package dockermr_test

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/codewandler/llm"
    "github.com/codewandler/llm/llmtest"
    "github.com/codewandler/llm/provider/dockermr"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNew_Defaults(t *testing.T) {
    p := dockermr.New()
    assert.Equal(t, "dockermr", p.Name())
    assert.NotEmpty(t, p.Models())
}

func TestFetchModels(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/engines/llama.cpp/v1/models", r.URL.Path)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(map[string]any{
            "data": []map[string]any{
                {"id": "ai/smollm2", "object": "model"},
                {"id": "ai/qwen2.5", "object": "model"},
            },
        })
    }))
    defer srv.Close()

    p := dockermr.New(llm.WithBaseURL(srv.URL))
    models, err := p.FetchModels(context.Background())
    require.NoError(t, err)
    require.Len(t, models, 2)
    assert.Equal(t, "ai/smollm2", models[0].ID)
    assert.Equal(t, "dockermr", models[0].Provider)
}

func TestCreateStream_Text(t *testing.T) {
    // Synthetic SSE fixture matching OpenAI Chat Completions wire format
    sseBody := strings.Join([]string{
        `data: {"id":"chatcmpl-1","model":"ai/smollm2","choices":[{"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
        `data: {"id":"chatcmpl-1","model":"ai/smollm2","choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
        `data: {"id":"chatcmpl-1","model":"ai/smollm2","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
        `data: [DONE]`,
        ``,
    }, "\n")

    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "text/event-stream")
        io.WriteString(w, sseBody)
    }))
    defer srv.Close()

    p := dockermr.New(llm.WithBaseURL(srv.URL))
    // ... build stream and collect events, assert text content
}

// Additional tests: tool calls, error paths, finish_reason cases
```

**Verification:**
```bash
go test -race ./provider/dockermr/...
go vet ./provider/dockermr/...
```

---

### Task 7 — Auto-detection in `provider/auto`

**Files modified**: `provider/auto/constants.go`, `provider/auto/detect.go`, `provider/auto/options.go`  
**Estimated time**: 8 minutes

**`constants.go`** — add:
```go
const (
    ProviderDockerMR    = "dockermr"
    // ...existing...
)
```

**`detect.go`** — add detection at end of `detectProviders()`:
```go
// N. Docker Model Runner — detect if localhost:12434 is reachable
if !disabled[ProviderDockerMR] {
    if dockerMRAvailable(httpClient) {
        providers = append(providers, providerEntry{
            name:         ProviderDockerMR,
            providerType: ProviderDockerMR,
            factory: func(opts ...llm.Option) llm.Provider {
                if httpClient != nil {
                    opts = append(opts, llm.WithHTTPClient(httpClient))
                }
                return dockermr.New(opts...)
            },
            modelAliases: nil,
            hasAliases:   false,
        })
    }
}
```

Add the probe function:
```go
// dockerMRAvailable returns true if the Docker Model Runner TCP endpoint
// responds to a model listing request within a short timeout.
func dockerMRAvailable(client *http.Client) bool {
    c := client
    if c == nil {
        c = &http.Client{Timeout: 500 * time.Millisecond}
    }
    resp, err := c.Get(dockermr.DefaultBaseURL + "/engines/llama.cpp/v1/models")
    if err != nil {
        return false
    }
    resp.Body.Close()
    return resp.StatusCode == http.StatusOK
}
```

**`options.go`** — add explicit opt-in:
```go
// WithDockerModelRunner adds the Docker Model Runner provider explicitly,
// bypassing auto-detection. This is useful when DMR is running on a
// non-default address (e.g. inside a container).
func WithDockerModelRunner(opts ...llm.Option) Option {
    return func(cfg *config) {
        cfg.providers = append(cfg.providers, providerEntry{
            name:         ProviderDockerMR,
            providerType: ProviderDockerMR,
            factory: func(extraOpts ...llm.Option) llm.Provider {
                return dockermr.New(append(opts, extraOpts...)...)
            },
            hasAliases: false,
        })
    }
}
```

**Verification:**
```bash
go build ./provider/auto/...
go vet ./provider/auto/...
go test ./provider/auto/...
```

---

### Task 8 — Wire into `llmcli` for manual smoke-testing

**Files modified**: (check `cmd/llmcli` — may already enumerate providers automatically)  
**Estimated time**: 2 minutes

Check if `cmd/llmcli` needs explicit registration or if it uses `auto.New()`. If the latter, auto-detection in Task 7 is sufficient.

```bash
go run ./cmd/llmcli infer -m ai/smollm2 "Hello from Docker Model Runner"
```

**Verification:**
```bash
go build ./...
go vet ./...
```

---

## File Summary

| File | Action |
|------|--------|
| `errors.go` | Add `ProviderNameDockerMR` constant |
| `provider/dockermr/models.go` | New — curated model list |
| `provider/dockermr/dockermr.go` | New — provider struct, New(), CreateStream(), FetchModels() |
| `provider/dockermr/request.go` | New — OpenAI-format request builder |
| `provider/dockermr/stream.go` | New — OpenAI-compatible SSE stream parser |
| `provider/dockermr/token_counter.go` | New — approximate token counter |
| `provider/dockermr/dockermr_test.go` | New — table-driven tests |
| `provider/auto/constants.go` | Add `ProviderDockerMR` |
| `provider/auto/detect.go` | Add DMR probe and detection |
| `provider/auto/options.go` | Add `WithDockerModelRunner()` |

---

## Estimated Total Time

~49 minutes implementation + testing

---

## Open questions before starting

1. **`internal/sse` API**: Confirm the scanner type and `Data()` / `Scan()` method names — look at how `provider/openai/api_completions.go` uses it, then mirror exactly.
2. **`usage.TokenItems.NonZero()`**: Confirm this method exists (it's used in Ollama).
3. **llmcli auto-detection**: Confirm `cmd/llmcli` uses `auto.New()` so no manual wiring is needed.
4. **Detection timeout**: A 500ms TCP probe is aggressive; consider if tests mock this or if a dedicated env var (`DOCKER_MODEL_RUNNER_URL`) should be the detection trigger instead.
