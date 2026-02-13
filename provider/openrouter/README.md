# OpenRouter Provider

OpenRouter provider for the LLM abstraction library.

## Configuration

```go
import "github.com/codewandler/llm/provider/openrouter"

// Create provider with default model (anthropic/claude-sonnet-4.5)
p := openrouter.New("your-api-key")

// Or specify a custom default model
p := openrouter.New("your-api-key").WithDefaultModel("openai/gpt-4o")

// Get the default model
defaultModel := p.DefaultModel()
```

## Constants

- `openrouter.DefaultModel` = `"anthropic/claude-sonnet-4.5"`

## Models

The `models.json` file contains a curated list of **229 models** that support **tool calling** on OpenRouter.

### Generation

The file was generated using the OpenRouter models API with **full model data**:

```bash
curl -s "https://openrouter.ai/api/v1/models" | \
  jq '[.data[] | select(.supported_parameters != null and (.supported_parameters | index("tools")))]' \
  > models.json
```

### Model Data Structure

Each model includes complete information:
- **ID and name**
- **Context length** and max completion tokens
- **Pricing** (prompt, completion, cache read)
- **Supported parameters** (tools, temperature, etc.)
- **Architecture** details (modality, tokenizer)
- **Default parameters**

### Accessing Model Data

```go
// Get basic model list (ID, Name, Provider)
models := p.Models()  // Returns 229 tool-enabled models

// Get detailed model data with pricing, context, etc.
modelData, err := openrouter.GetModelData()
for _, m := range modelData {
    fmt.Printf("%s: %d tokens, $%s per prompt token\n", 
        m.Name, m.ContextLength, m.Pricing.Prompt)
}
```

### Contents

The file includes popular models with tool support from:
- **Anthropic** (Claude Opus, Sonnet, Haiku variants)
- **OpenAI** (GPT-4, GPT-4o, o3-mini, etc.)
- **Google** (Gemini models)
- **Meta** (Llama models)
- **Mistral** (Mistral, Mixtral models)
- **Qwen** (Qwen3 models)
- **DeepSeek** (DeepSeek models)
- And many more providers (229 total)

### Usage

The embedded `Models()` function returns all 229 tool-enabled models from `models.json`. For real-time data from the API, use `FetchModels()`.
