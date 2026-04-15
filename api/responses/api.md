# OpenAI Responses API — Package Reference

> Source: `api/responses/types.go`, `api/responses/constants.go`, `api/responses/parser.go`  
> Upstream: <https://platform.openai.com/docs/api-reference/responses/create>  
> Streaming events: <https://developers.openai.com/api/reference/resources/responses/streaming-events>

---

## Request Schema

JSON schema for `POST /v1/responses`, derived directly from `types.go`.

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Request",
  "type": "object",
  "required": ["model", "input", "stream"],
  "properties": {
    "model":                  { "type": "string" },
    "input":                  { "type": "array", "items": { "$ref": "#/definitions/Input" } },
    "instructions":           { "type": "string" },
    "tools":                  { "type": "array", "items": { "$ref": "#/definitions/Tool" } },
    "tool_choice":            { "description": "\"auto\"|\"none\"|\"required\" or {\"type\":\"function\",\"name\":\"…\"}" },
    "reasoning":              { "$ref": "#/definitions/Reasoning" },
    "max_output_tokens":      { "type": "integer" },
    "temperature":            { "type": "number" },
    "top_p":                  { "type": "number" },
    "top_k":                  { "type": "integer" },
    "response_format":        { "$ref": "#/definitions/ResponseFormat" },
    "prompt_cache_retention": { "type": "string" },
    "stream":                 { "type": "boolean" }
  },
  "definitions": {
    "Input": {
      "type": "object",
      "properties": {
        "role":      { "type": "string", "enum": ["user","assistant","system","developer"] },
        "content":   { "type": "string" },
        "type":      { "type": "string", "description": "e.g. \"function_call_output\"" },
        "call_id":   { "type": "string" },
        "name":      { "type": "string" },
        "arguments": { "type": "string" },
        "output":    { "type": "string" }
      }
    },
    "Tool": {
      "type": "object",
      "required": ["type", "name"],
      "properties": {
        "type":        { "type": "string", "enum": ["function"] },
        "name":        { "type": "string" },
        "description": { "type": "string" },
        "parameters":  { "description": "JSON Schema object" },
        "strict":      { "type": "boolean" }
      }
    },
    "Reasoning": {
      "type": "object",
      "properties": {
        "effort":  { "type": "string", "enum": ["low","medium","high"] },
        "summary": { "type": "string", "enum": ["auto","concise","detailed"] }
      }
    },
    "ResponseFormat": {
      "type": "object",
      "required": ["type"],
      "properties": {
        "type": { "type": "string", "enum": ["text","json_object"] }
      }
    }
  }
}
```

### Example Request

```json
{
  "model": "gpt-4o",
  "stream": true,
  "instructions": "You are a helpful assistant. Be concise.",
  "input": [
    { "role": "user", "content": "What is the weather in Berlin right now?" }
  ],
  "tools": [
    {
      "type": "function",
      "name": "get_weather",
      "description": "Returns current weather for a city.",
      "parameters": {
        "type": "object",
        "properties": { "city": { "type": "string" } },
        "required": ["city"]
      },
      "strict": true
    }
  ],
  "tool_choice": "auto",
  "max_output_tokens": 1024,
  "temperature": 0.7
}
```

---

## Shared Embedded Types

Every event payload is a flat JSON object — embedded Go structs are inlined. These are the building blocks:

**`EventMeta`** — present in **all** events:
| JSON field | Type | Notes |
|---|---|---|
| `type` | `string` | The SSE event type string |
| `sequence_number` | `int` | Monotonically increasing per stream; omitted when 0 |

**`OutputRef`** — output-item scope (most tool/text events):
| JSON field | Type |
|---|---|
| `output_index` | `int` |
| `item_id` | `string` |

**`ContentRef`** — content-part scope (text/refusal events):
| JSON field | Type |
|---|---|
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |

**`SummaryRef`** — reasoning summary scope:
| JSON field | Type |
|---|---|
| `output_index` | `int` |
| `item_id` | `string` |
| `summary_index` | `int` |

**`ResponseRef`** — audio events:
| JSON field | Type |
|---|---|
| `response_id` | `string` |

---

## Streaming Events

### Response Lifecycle

---

#### `response.created` · `ResponseCreatedEvent`

Emitted first in every stream. The response object has `status: "in_progress"` and no output yet.

| JSON field | Type |
|---|---|
| `type` | `"response.created"` |
| `sequence_number` | `int` |
| `response.id` | `string` |
| `response.model` | `string` |
| `response.created_at` | `int64` (unix seconds) |
| `response.status` | `string` |
| `response.error` | `{code, message}` or null |
| `response.incomplete_details` | `{reason}` or null |
| `response.instructions` | `string` or array or null |
| `response.output` | `[]ResponseOutputItem` |
| `response.usage` | `{input_tokens, output_tokens, …}` or null |
| `response.user` | `string` |
| `response.metadata` | `object` |

```json
{
  "type": "response.created",
  "sequence_number": 1,
  "response": {
    "id": "resp_abc123",
    "model": "gpt-4o",
    "created_at": 1710000000,
    "status": "in_progress",
    "output": [],
    "usage": null
  }
}
```

---

#### `response.queued` · `ResponseQueuedEvent`

Emitted when the response is queued waiting to be processed (same shape as `response.created`).

```json
{
  "type": "response.queued",
  "sequence_number": 1,
  "response": {
    "id": "resp_abc123",
    "model": "gpt-4o",
    "created_at": 1710000000,
    "status": "queued"
  }
}
```

---

#### `response.in_progress` · `ResponseInProgressEvent`

Emitted while the response is actively running (same shape as `response.created`).

```json
{
  "type": "response.in_progress",
  "sequence_number": 2,
  "response": {
    "id": "resp_abc123",
    "model": "gpt-4o",
    "created_at": 1710000000,
    "status": "in_progress"
  }
}
```

---

#### `response.completed` · `ResponseCompletedEvent`

Terminal success event. Note: uses a **narrower** inline struct (not `ResponsePayload`) — only the fields below are present.

| JSON field | Type |
|---|---|
| `type` | `"response.completed"` |
| `sequence_number` | `int` |
| `response.id` | `string` |
| `response.model` | `string` |
| `response.status` | `"completed"` |
| `response.incomplete_details` | `{reason}` or null |
| `response.error` | `{code, message}` or null |
| `response.usage.input_tokens` | `int` |
| `response.usage.output_tokens` | `int` |
| `response.usage.input_tokens_details.cached_tokens` | `int` |
| `response.usage.output_tokens_details.reasoning_tokens` | `int` |

```json
{
  "type": "response.completed",
  "sequence_number": 42,
  "response": {
    "id": "resp_abc123",
    "model": "gpt-4o",
    "status": "completed",
    "usage": {
      "input_tokens": 512,
      "output_tokens": 128,
      "input_tokens_details": { "cached_tokens": 256 },
      "output_tokens_details": { "reasoning_tokens": 0 }
    }
  }
}
```

---

#### `response.failed` · `ResponseFailedEvent`

Terminal event when the API could not produce a response.

```json
{
  "type": "response.failed",
  "sequence_number": 5,
  "response": {
    "id": "resp_abc123",
    "model": "gpt-4o",
    "status": "failed",
    "error": {
      "code": "server_error",
      "message": "An internal server error occurred."
    }
  }
}
```

---

#### `response.incomplete` · `ResponseIncompleteEvent`

Terminal event when output stopped before completion (e.g. hit `max_output_tokens` or content filter).

```json
{
  "type": "response.incomplete",
  "sequence_number": 38,
  "response": {
    "id": "resp_abc123",
    "model": "gpt-4o",
    "status": "incomplete",
    "incomplete_details": { "reason": "max_output_tokens" }
  }
}
```

---

### Output Item Lifecycle

---

#### `response.output_item.added` · `OutputItemAddedEvent`

Emitted when a new output item begins. The `item.type` tells you what kind: `"message"`, `"function_call"`, `"file_search_call"`, `"web_search_call"`, `"reasoning"`, etc.

| JSON field | Type |
|---|---|
| `type` | `"response.output_item.added"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item.id` | `string` |
| `item.type` | `string` |
| `item.status` | `string` |
| `item.role` | `string` |
| `item.content` | `[]ResponseContentPart` |
| `item.call_id` | `string` |
| `item.name` | `string` |
| `item.arguments` | `string` |
| `item.output` | `string` |
| `item.input` | `string` |
| `item.results` | `[]object` |
| `item.summary` | `[]ReasoningSummaryPart` |
| `item.queries` | `[]string` |
| `item.code` | `string` |
| `item.container_id` | `string` |
| `item.file_id` | `string` |
| `item.server_label` | `string` |
| `item.tool_name` | `string` |

```json
{
  "type": "response.output_item.added",
  "sequence_number": 3,
  "output_index": 0,
  "item": {
    "id": "msg_xyz",
    "type": "message",
    "status": "in_progress",
    "role": "assistant",
    "content": []
  }
}
```

---

#### `response.output_item.done` · `OutputItemDoneEvent`

Same shape as `output_item.added`. Emitted when an output item is fully complete with all its content.

```json
{
  "type": "response.output_item.done",
  "sequence_number": 25,
  "output_index": 0,
  "item": {
    "id": "msg_xyz",
    "type": "message",
    "status": "completed",
    "role": "assistant",
    "content": [
      { "type": "output_text", "text": "The weather in Berlin is 18°C and partly cloudy." }
    ]
  }
}
```

---

### Content Part Lifecycle

---

#### `response.content_part.added` · `ContentPartAddedEvent`

Emitted when a new content part starts within an output message item.

| JSON field | Type |
|---|---|
| `type` | `"response.content_part.added"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |
| `part.type` | `string` (`"output_text"`, `"refusal"`, etc.) |
| `part.text` | `string` |
| `part.refusal` | `string` |
| `part.annotations` | `[]OutputTextAnnotation` |
| `part.logprobs` | `[]TokenLogprob` |
| `part.transcript` | `string` |

```json
{
  "type": "response.content_part.added",
  "sequence_number": 4,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "part": { "type": "output_text", "text": "" }
}
```

---

#### `response.content_part.done` · `ContentPartDoneEvent`

Same shape as `content_part.added`. Emitted when the content part is fully finalized.

```json
{
  "type": "response.content_part.done",
  "sequence_number": 24,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "part": {
    "type": "output_text",
    "text": "The weather in Berlin is 18°C and partly cloudy."
  }
}
```

---

### Text Output

---

#### `response.output_text.delta` · `OutputTextDeltaEvent`

Incremental text chunk. Stream and append `delta` to reconstruct the full text.

| JSON field | Type |
|---|---|
| `type` | `"response.output_text.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |
| `delta` | `string` |
| `logprobs` | `[]TokenLogprob` (optional) |

```json
{
  "type": "response.output_text.delta",
  "sequence_number": 10,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "delta": "The weather"
}
```

---

#### `response.output_text.done` · `OutputTextDoneEvent`

Finalized full text for a content part. `text` is the complete concatenation.

| JSON field | Type |
|---|---|
| `type` | `"response.output_text.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |
| `text` | `string` |
| `logprobs` | `[]TokenLogprob` (optional) |

```json
{
  "type": "response.output_text.done",
  "sequence_number": 23,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "text": "The weather in Berlin is 18°C and partly cloudy."
}
```

---

#### `response.output_text.annotation.added` · `OutputTextAnnotationAddedEvent`

Emitted when a citation or file-path annotation is attached to output text.

| JSON field | Type |
|---|---|
| `type` | `"response.output_text.annotation.added"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |
| `annotation_index` | `int` |
| `annotation.type` | `string` (`"file_citation"`, `"url_citation"`, `"file_path"`, `"container_file_citation"`) |
| `annotation.file_id` | `string` |
| `annotation.filename` | `string` |
| `annotation.index` | `int` |
| `annotation.start_index` | `int` |
| `annotation.end_index` | `int` |
| `annotation.title` | `string` |
| `annotation.url` | `string` |
| `annotation.container_id` | `string` |
| `annotation.offset` | `int` |
| `annotation.text` | `string` |

```json
{
  "type": "response.output_text.annotation.added",
  "sequence_number": 15,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "annotation_index": 0,
  "annotation": {
    "type": "url_citation",
    "start_index": 11,
    "end_index": 42,
    "title": "Berlin Weather - Weather.com",
    "url": "https://weather.com/berlin"
  }
}
```

---

### Refusal

---

#### `response.refusal.delta` · `RefusalDeltaEvent`

Incremental refusal text chunk.

| JSON field | Type |
|---|---|
| `type` | `"response.refusal.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |
| `delta` | `string` |

```json
{
  "type": "response.refusal.delta",
  "sequence_number": 5,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "delta": "I'm sorry, I can't"
}
```

---

#### `response.refusal.done` · `RefusalDoneEvent`

Finalized refusal text.

| JSON field | Type |
|---|---|
| `type` | `"response.refusal.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `content_index` | `int` |
| `refusal` | `string` |

```json
{
  "type": "response.refusal.done",
  "sequence_number": 12,
  "output_index": 0,
  "item_id": "msg_xyz",
  "content_index": 0,
  "refusal": "I'm sorry, I can't help with that request."
}
```

---

### Function Calls

---

#### `response.function_call_arguments.delta` · `FunctionCallArgumentsDeltaEvent`

Streamed fragment of JSON arguments for a function call.

| JSON field | Type |
|---|---|
| `type` | `"response.function_call_arguments.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.function_call_arguments.delta",
  "sequence_number": 8,
  "output_index": 0,
  "item_id": "fc_abc",
  "delta": "{\"city\":"
}
```

---

#### `response.function_call_arguments.done` · `FunctionCallArgumentsDoneEvent`

Finalized function call. `arguments` is the complete JSON string. Use `call_id` from the preceding `output_item.added` to match tool results.

| JSON field | Type |
|---|---|
| `type` | `"response.function_call_arguments.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `name` | `string` |
| `arguments` | `string` |

```json
{
  "type": "response.function_call_arguments.done",
  "sequence_number": 14,
  "output_index": 0,
  "item_id": "fc_abc",
  "name": "get_weather",
  "arguments": "{\"city\": \"Berlin\"}"
}
```

---

### File Search Tool

---

#### `response.file_search_call.in_progress` · `FileSearchCallInProgressEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.file_search_call.in_progress"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |

```json
{
  "type": "response.file_search_call.in_progress",
  "sequence_number": 4,
  "output_index": 0,
  "item_id": "fs_abc"
}
```

---

#### `response.file_search_call.searching` · `FileSearchCallSearchingEvent`

```json
{
  "type": "response.file_search_call.searching",
  "sequence_number": 5,
  "output_index": 0,
  "item_id": "fs_abc"
}
```

---

#### `response.file_search_call.completed` · `FileSearchCallCompletedEvent`

```json
{
  "type": "response.file_search_call.completed",
  "sequence_number": 9,
  "output_index": 0,
  "item_id": "fs_abc"
}
```

---

### Web Search Tool

---

#### `response.web_search_call.in_progress` · `WebSearchCallInProgressEvent`

```json
{
  "type": "response.web_search_call.in_progress",
  "sequence_number": 4,
  "output_index": 0,
  "item_id": "ws_abc"
}
```

---

#### `response.web_search_call.searching` · `WebSearchCallSearchingEvent`

```json
{
  "type": "response.web_search_call.searching",
  "sequence_number": 5,
  "output_index": 0,
  "item_id": "ws_abc"
}
```

---

#### `response.web_search_call.completed` · `WebSearchCallCompletedEvent`

```json
{
  "type": "response.web_search_call.completed",
  "sequence_number": 9,
  "output_index": 0,
  "item_id": "ws_abc"
}
```

---

### Reasoning Summary

---

#### `response.reasoning_summary_part.added` · `ReasoningSummaryPartAddedEvent`

Emitted when a new reasoning summary part begins (for models like o3 with `reasoning.summary` enabled).

| JSON field | Type |
|---|---|
| `type` | `"response.reasoning_summary_part.added"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `summary_index` | `int` |
| `part.type` | `string` (`"summary_text"`) |
| `part.text` | `string` |

```json
{
  "type": "response.reasoning_summary_part.added",
  "sequence_number": 3,
  "output_index": 0,
  "item_id": "rs_abc",
  "summary_index": 0,
  "part": { "type": "summary_text", "text": "" }
}
```

---

#### `response.reasoning_summary_part.done` · `ReasoningSummaryPartDoneEvent`

```json
{
  "type": "response.reasoning_summary_part.done",
  "sequence_number": 18,
  "output_index": 0,
  "item_id": "rs_abc",
  "summary_index": 0,
  "part": { "type": "summary_text", "text": "The user is asking about Berlin weather. I should call get_weather." }
}
```

---

#### `response.reasoning_summary_text.delta` · `ReasoningSummaryTextDeltaEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.reasoning_summary_text.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.reasoning_summary_text.delta",
  "sequence_number": 7,
  "output_index": 0,
  "item_id": "rs_abc",
  "delta": "The user is asking"
}
```

---

#### `response.reasoning_summary_text.done` · `ReasoningSummaryTextDoneEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.reasoning_summary_text.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `text` | `string` |

```json
{
  "type": "response.reasoning_summary_text.done",
  "sequence_number": 17,
  "output_index": 0,
  "item_id": "rs_abc",
  "text": "The user is asking about Berlin weather. I should call get_weather."
}
```

---

### Reasoning Text (Internal)

Raw internal reasoning/thinking tokens. Only emitted when the model exposes its chain-of-thought.

---

#### `response.reasoning_text.delta` · `ReasoningTextDeltaEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.reasoning_text.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.reasoning_text.delta",
  "sequence_number": 5,
  "output_index": 0,
  "item_id": "rt_abc",
  "delta": "Let me think about this..."
}
```

---

#### `response.reasoning_text.done` · `ReasoningTextDoneEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.reasoning_text.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `text` | `string` |

```json
{
  "type": "response.reasoning_text.done",
  "sequence_number": 16,
  "output_index": 0,
  "item_id": "rt_abc",
  "text": "Let me think about this... The user wants weather data for Berlin."
}
```

---

### Image Generation Tool

---

#### `response.image_generation_call.in_progress` · `ImageGenerationCallInProgressEvent`

```json
{
  "type": "response.image_generation_call.in_progress",
  "sequence_number": 3,
  "output_index": 0,
  "item_id": "ig_abc"
}
```

---

#### `response.image_generation_call.generating` · `ImageGenerationCallGeneratingEvent`

```json
{
  "type": "response.image_generation_call.generating",
  "sequence_number": 4,
  "output_index": 0,
  "item_id": "ig_abc"
}
```

---

#### `response.image_generation_call.completed` · `ImageGenerationCallCompletedEvent`

```json
{
  "type": "response.image_generation_call.completed",
  "sequence_number": 20,
  "output_index": 0,
  "item_id": "ig_abc"
}
```

---

#### `response.image_generation_call.partial_image` · `ImageGenerationCallPartialImageEvent`

Carries a base64-encoded partial image during streaming image generation.

| JSON field | Type |
|---|---|
| `type` | `"response.image_generation_call.partial_image"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `partial_image_index` | `int` |
| `partial_image_b64` | `string` (base64) |

```json
{
  "type": "response.image_generation_call.partial_image",
  "sequence_number": 10,
  "output_index": 0,
  "item_id": "ig_abc",
  "partial_image_index": 2,
  "partial_image_b64": "iVBORw0KGgoAAAANSUhEUgAA..."
}
```

---

### MCP Tool Calls

---

#### `response.mcp_call.in_progress` · `MCPCallInProgressEvent`

```json
{
  "type": "response.mcp_call.in_progress",
  "sequence_number": 4,
  "output_index": 0,
  "item_id": "mcp_abc"
}
```

---

#### `response.mcp_call.failed` · `MCPCallFailedEvent`

```json
{
  "type": "response.mcp_call.failed",
  "sequence_number": 9,
  "output_index": 0,
  "item_id": "mcp_abc"
}
```

---

#### `response.mcp_call.completed` · `MCPCallCompletedEvent`

```json
{
  "type": "response.mcp_call.completed",
  "sequence_number": 12,
  "output_index": 0,
  "item_id": "mcp_abc"
}
```

---

#### `response.mcp_call_arguments.delta` · `MCPCallArgumentsDeltaEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.mcp_call_arguments.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.mcp_call_arguments.delta",
  "sequence_number": 6,
  "output_index": 0,
  "item_id": "mcp_abc",
  "delta": "{\"repo\":"
}
```

---

#### `response.mcp_call_arguments.done` · `MCPCallArgumentsDoneEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.mcp_call_arguments.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `arguments` | `string` |

```json
{
  "type": "response.mcp_call_arguments.done",
  "sequence_number": 8,
  "output_index": 0,
  "item_id": "mcp_abc",
  "arguments": "{\"repo\": \"openai/openai-python\"}"
}
```

---

### MCP Tool Discovery

---

#### `response.mcp_list_tools.in_progress` · `MCPListToolsInProgressEvent`

```json
{
  "type": "response.mcp_list_tools.in_progress",
  "sequence_number": 2,
  "output_index": 0,
  "item_id": "mcplt_abc"
}
```

---

#### `response.mcp_list_tools.failed` · `MCPListToolsFailedEvent`

```json
{
  "type": "response.mcp_list_tools.failed",
  "sequence_number": 3,
  "output_index": 0,
  "item_id": "mcplt_abc"
}
```

---

#### `response.mcp_list_tools.completed` · `MCPListToolsCompletedEvent`

```json
{
  "type": "response.mcp_list_tools.completed",
  "sequence_number": 3,
  "output_index": 0,
  "item_id": "mcplt_abc"
}
```

---

### Code Interpreter Tool

---

#### `response.code_interpreter_call.in_progress` · `CodeInterpreterCallInProgressEvent`

```json
{
  "type": "response.code_interpreter_call.in_progress",
  "sequence_number": 4,
  "output_index": 0,
  "item_id": "ci_abc"
}
```

---

#### `response.code_interpreter_call.interpreting` · `CodeInterpreterCallInterpretingEvent`

```json
{
  "type": "response.code_interpreter_call.interpreting",
  "sequence_number": 8,
  "output_index": 0,
  "item_id": "ci_abc"
}
```

---

#### `response.code_interpreter_call.completed` · `CodeInterpreterCallCompletedEvent`

```json
{
  "type": "response.code_interpreter_call.completed",
  "sequence_number": 22,
  "output_index": 0,
  "item_id": "ci_abc"
}
```

---

#### `response.code_interpreter_call_code.delta` · `CodeInterpreterCallCodeDeltaEvent`

Streamed code snippet fragment.

| JSON field | Type |
|---|---|
| `type` | `"response.code_interpreter_call_code.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.code_interpreter_call_code.delta",
  "sequence_number": 10,
  "output_index": 0,
  "item_id": "ci_abc",
  "delta": "import pandas as pd\n"
}
```

---

#### `response.code_interpreter_call_code.done` · `CodeInterpreterCallCodeDoneEvent`

Finalized complete code snippet.

| JSON field | Type |
|---|---|
| `type` | `"response.code_interpreter_call_code.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `code` | `string` |

```json
{
  "type": "response.code_interpreter_call_code.done",
  "sequence_number": 21,
  "output_index": 0,
  "item_id": "ci_abc",
  "code": "import pandas as pd\ndf = pd.read_csv('data.csv')\nprint(df.describe())"
}
```

---

### Custom Tool Calls

---

#### `response.custom_tool_call_input.delta` · `CustomToolCallInputDeltaEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.custom_tool_call_input.delta"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.custom_tool_call_input.delta",
  "sequence_number": 6,
  "output_index": 0,
  "item_id": "ct_abc",
  "delta": "{\"query\":"
}
```

---

#### `response.custom_tool_call_input.done` · `CustomToolCallInputDoneEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.custom_tool_call_input.done"` |
| `sequence_number` | `int` |
| `output_index` | `int` |
| `item_id` | `string` |
| `input` | `string` |

```json
{
  "type": "response.custom_tool_call_input.done",
  "sequence_number": 11,
  "output_index": 0,
  "item_id": "ct_abc",
  "input": "{\"query\": \"latest Berlin weather\"}"
}
```

---

### Audio

---

#### `response.audio.delta` · `AudioDeltaEvent`

Incremental base64-encoded audio chunk.

| JSON field | Type |
|---|---|
| `type` | `"response.audio.delta"` |
| `sequence_number` | `int` |
| `response_id` | `string` |
| `delta` | `string` (base64) |

```json
{
  "type": "response.audio.delta",
  "sequence_number": 5,
  "response_id": "resp_abc123",
  "delta": "UklGRiQAAABXQVZFZm10IBAAAA..."
}
```

---

#### `response.audio.done` · `AudioDoneEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.audio.done"` |
| `sequence_number` | `int` |
| `response_id` | `string` |

```json
{
  "type": "response.audio.done",
  "sequence_number": 30,
  "response_id": "resp_abc123"
}
```

---

#### `response.audio.transcript.delta` · `AudioTranscriptDeltaEvent`

Incremental audio transcript text.

| JSON field | Type |
|---|---|
| `type` | `"response.audio.transcript.delta"` |
| `sequence_number` | `int` |
| `response_id` | `string` |
| `delta` | `string` |

```json
{
  "type": "response.audio.transcript.delta",
  "sequence_number": 6,
  "response_id": "resp_abc123",
  "delta": "The weather in"
}
```

---

#### `response.audio.transcript.done` · `AudioTranscriptDoneEvent`

| JSON field | Type |
|---|---|
| `type` | `"response.audio.transcript.done"` |
| `sequence_number` | `int` |
| `response_id` | `string` |

```json
{
  "type": "response.audio.transcript.done",
  "sequence_number": 29,
  "response_id": "resp_abc123"
}
```

---

### Errors

---

#### `error` · `APIErrorEvent`

Stream-level error. Implements the `error` interface via `APIErrorEvent.Error()`.

| JSON field | Type |
|---|---|
| `type` | `"error"` |
| `sequence_number` | `int` |
| `code` | `string` |
| `message` | `string` |
| `param` | `any` |

```json
{
  "type": "error",
  "sequence_number": 7,
  "code": "rate_limit_exceeded",
  "message": "You have exceeded your rate limit. Please retry after 60 seconds.",
  "param": null
}
```
