# Plan: migrate provider/codex to providercore — exact code changes

Scope: `provider/codex/` only. No other packages are touched.

---

## Files affected

| File | Change |
|---|---|
| `provider/codex/provider.go` | Rewrite (see steps 1–7 below) |
| `provider/codex/token_counter.go` | No change |
| `provider/codex/auth.go` | No change |
| `provider/codex/models.go` | No change |
| `provider/codex/source.go` | No change |
| `provider/codex/integration_test.go` | No change |

---

## Known blocker — tool-name and call-id resolution

Before executing the `provider/codex` migration, a bug in
`api/unified/stream_bridge_responses.go` must be fixed.

### Is this Codex-specific?

No. The `name` field in `FunctionCallArgumentsDoneEvent` is
`json:"name,omitempty"` in `api/responses/types.go` — it is optional by
design. The existing `api/responses` integration test already reads tool
calls from `OutputItemDoneEvent`, not `FunctionCallArgumentsDoneEvent`,
precisely because `output_item.done` is the only event that *always*
carries both `name` and `call_id`. Any provider can legitimately omit
`name` from `function_call_arguments.done`.

### Should the fix go in `api/responses`?

No. `api/responses` is a thin JSON-deserialisation layer that faithfully
reflects the wire. `FunctionCallArgumentsDoneEvent` already models the
wire correctly (`Name omitempty`). The incorrect assumption that `name` is
always present lives in `api/unified`.

### Exact wire events captured from the Codex API

```
event: response.output_item.added
data: {"item":{"id":"fc_...","type":"function_call",
              "call_id":"call_...","name":"get_weather"},
       "output_index":0}
           ^ name and call_id ARE present

event: response.function_call_arguments.delta
data: {"delta":"{\"location\"","item_id":"fc_...","output_index":0}
           ^ no name, no call_id

event: response.function_call_arguments.done
data: {"arguments":"{\"location\":\"Tokyo\"}","item_id":"fc_...","output_index":0}
           ^ no name, no call_id

event: response.output_item.done
data: {"item":{"id":"fc_...","call_id":"call_...",
              "name":"get_weather","arguments":"{...}"},
       "output_index":0}
           ^ name and call_id present again
```

Two problems in the current `MapResponsesEvent` `FunctionCallArgumentsDoneEvent` case:

1. `Name: e.Name` — empty for Codex (not sent in this event)
2. `ID: e.ItemID` — uses internal item ID (`"fc_..."`), not the
   tool-call ID (`"call_..."`). Tool result messages must reference
   `call_id` to match the call, not `item_id`.

### Root cause

```go
// CURRENT — broken for providers that omit name/call_id from .done
case *responses.FunctionCallArgumentsDoneEvent:
    var args map[string]any
    _ = json.Unmarshal([]byte(e.Arguments), &args)
    return ..., StreamEvent{
        StreamToolCall: &StreamToolCall{
            ID:   e.ItemID, // item_id, not call_id
            Name: e.Name,   // empty on Codex
        },
        ToolCall: &ToolCall{
            ID:   e.ItemID,
            Name: e.Name,
        },
    }, ...
```

`MapResponsesEvent` is a **stateless** function — it cannot see what
`OutputItemAddedEvent` emitted earlier in the same stream.

### Fix: `ResponsesMapper` in `api/unified/stream_bridge_responses.go`

Convert `MapResponsesEvent` from a stateless function into a method on a
new `ResponsesMapper` struct that tracks `{name, callID}` from
`output_item.added` (keyed by `output_index`) and back-fills them when
`function_call_arguments.done` arrives.

#### 1. Add types (top of `stream_bridge_responses.go`)

```go
// funcCallMeta holds function-call metadata from response.output_item.added
// that may be absent from the later response.function_call_arguments.done.
type funcCallMeta struct {
    name   string
    callID string
}

// ResponsesMapper converts Responses API events to unified StreamEvents.
// It is stateful: it carries function-call name and call_id forward from
// response.output_item.added so that response.function_call_arguments.done
// events from providers that omit those fields (e.g. Codex) still produce
// correct ToolCall events.
type ResponsesMapper struct {
    pending map[int]funcCallMeta // keyed by output_index
}

// NewResponsesMapper returns an initialised mapper for one stream.
func NewResponsesMapper() *ResponsesMapper {
    return &ResponsesMapper{pending: make(map[int]funcCallMeta)}
}
```

#### 2. Convert `MapResponsesEvent` to a method

```go
// BEFORE
func MapResponsesEvent(ev any) (StreamEvent, bool, error) {

// AFTER
func (m *ResponsesMapper) MapEvent(ev any) (StreamEvent, bool, error) {
```

The entire function body is unchanged except for the two case changes below.

#### 3. `OutputItemAddedEvent` case — record metadata

```go
// BEFORE
case *responses.OutputItemAddedEvent:
    return withRawEventPayload(withProviderExtras(withRawEventName(
        StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{
            Scope: LifecycleScopeItem, State: LifecycleStateAdded,
            Ref: responsesItemRef(e.OutputIndex, e.Item.ID), ItemType: e.Item.Type,
        }}, responses.EventOutputItemAdded),
        map[string]any{"item": e.Item}), source), false, nil

// AFTER
case *responses.OutputItemAddedEvent:
    if e.Item.Type == "function_call" && (e.Item.Name != "" || e.Item.CallID != "") {
        m.pending[e.OutputIndex] = funcCallMeta{
            name:   e.Item.Name,
            callID: e.Item.CallID,
        }
    }
    return withRawEventPayload(withProviderExtras(withRawEventName(
        StreamEvent{Type: StreamEventLifecycle, Lifecycle: &Lifecycle{
            Scope: LifecycleScopeItem, State: LifecycleStateAdded,
            Ref: responsesItemRef(e.OutputIndex, e.Item.ID), ItemType: e.Item.Type,
        }}, responses.EventOutputItemAdded),
        map[string]any{"item": e.Item}), source), false, nil
```

#### 4. `FunctionCallArgumentsDoneEvent` case — consume metadata

```go
// BEFORE
case *responses.FunctionCallArgumentsDoneEvent:
    var args map[string]any
    _ = json.Unmarshal([]byte(e.Arguments), &args)
    return withRawEventPayload(withRawEventName(StreamEvent{
        Type: StreamEventToolCall,
        StreamToolCall: &StreamToolCall{
            Ref: responsesItemRef(e.OutputIndex, e.ItemID),
            ID: e.ItemID, Name: e.Name, RawInput: e.Arguments, Args: args,
        },
        ToolCall: &ToolCall{ID: e.ItemID, Name: e.Name, Args: args},
    }, responses.EventFunctionCallArgumentsDone), source), false, nil

// AFTER
case *responses.FunctionCallArgumentsDoneEvent:
    var args map[string]any
    _ = json.Unmarshal([]byte(e.Arguments), &args)
    // Resolve name and call_id: providers such as Codex omit these from
    // function_call_arguments.done; carry them from output_item.added.
    name, callID := e.Name, e.ItemID
    if meta, ok := m.pending[e.OutputIndex]; ok {
        if name == "" {
            name = meta.name
        }
        if meta.callID != "" {
            // Always prefer the explicit call_id over item_id:
            // tool result messages reference call_id, not item_id.
            callID = meta.callID
        }
        delete(m.pending, e.OutputIndex)
    }
    return withRawEventPayload(withRawEventName(StreamEvent{
        Type: StreamEventToolCall,
        StreamToolCall: &StreamToolCall{
            Ref:      responsesItemRef(e.OutputIndex, e.ItemID),
            ID:       callID,
            Name:     name,
            RawInput: e.Arguments,
            Args:     args,
        },
        ToolCall: &ToolCall{ID: callID, Name: name, Args: args},
    }, responses.EventFunctionCallArgumentsDone), source), false, nil
```

#### 5. Keep backward-compatible package-level wrapper

All existing tests call `MapResponsesEvent` as a free function.

```go
// MapResponsesEvent is a stateless convenience wrapper around
// ResponsesMapper.MapEvent. For streaming use prefer NewResponsesMapper().MapEvent
// so that state is preserved across events in the same stream.
func MapResponsesEvent(ev any) (StreamEvent, bool, error) {
    return NewResponsesMapper().MapEvent(ev)
}
```

#### 6. Update `ForwardResponses` in `stream_forward_responses.go`

```go
// BEFORE
for result := range handle.Events {
    ...
    uEv, ignored, err := MapResponsesEvent(result)

// AFTER  (one new line before the loop, one changed line inside it)
mapper := NewResponsesMapper()
for result := range handle.Events {
    ...
    uEv, ignored, err := mapper.MapEvent(result)
```

#### 7. New test in `stream_bridge_responses_test.go`

```go
func TestResponsesMapper_FillsMissingFuncCallMeta(t *testing.T) {
    // Codex style: name and call_id only in output_item.added, absent from .done
    mapper := NewResponsesMapper()

    _, _, err := mapper.MapEvent(&responses.OutputItemAddedEvent{
        OutputIndex: 0,
        Item: responses.ResponseOutputItem{
            ID: "fc_abc", Type: "function_call",
            Name: "get_weather", CallID: "call_xyz",
        },
    })
    require.NoError(t, err)

    ev, _, err := mapper.MapEvent(&responses.FunctionCallArgumentsDoneEvent{
        OutputRef: responses.OutputRef{OutputIndex: 0, ItemID: "fc_abc"},
        Name:      "",    // not sent by Codex
        Arguments: `{"location":"Tokyo"}`,
    })
    require.NoError(t, err)
    require.Equal(t, StreamEventToolCall, ev.Type)
    require.NotNil(t, ev.ToolCall)
    assert.Equal(t, "get_weather", ev.ToolCall.Name, "name filled from output_item.added")
    assert.Equal(t, "call_xyz",    ev.ToolCall.ID,   "ID is call_id, not item_id")

    // Standard OpenAI style: name present in .done, no prior tracking needed
    mapper2 := NewResponsesMapper()
    ev2, _, err := mapper2.MapEvent(&responses.FunctionCallArgumentsDoneEvent{
        OutputRef: responses.OutputRef{OutputIndex: 0, ItemID: "call_1"},
        Name:      "lookup",
        Arguments: `{"a":1}`,
    })
    require.NoError(t, err)
    assert.Equal(t, "lookup", ev2.ToolCall.Name, "existing behaviour preserved")
}
```

### Files changed for this prerequisite

| File | Change |
|---|---|
| `api/unified/stream_bridge_responses.go` | Add `ResponsesMapper`, `funcCallMeta`, `NewResponsesMapper`; convert to method; record in `OutputItemAddedEvent`; resolve in `FunctionCallArgumentsDoneEvent`; keep `MapResponsesEvent` wrapper |
| `api/unified/stream_forward_responses.go` | `mapper := NewResponsesMapper()` before loop; `mapper.MapEvent(result)` inside loop |
| `api/unified/stream_bridge_responses_test.go` | Add `TestResponsesMapper_FillsMissingFuncCallMeta` |

No changes to `api/responses/` — it is correct as-is.

---


## Step 1 — Update imports in `provider.go`

**Remove:**
```go
"github.com/codewandler/llm/provider/openai"
```

**Add:**
```go
"github.com/codewandler/llm/provider/providercore"
```

All other existing imports stay (`bytes`, `io`, `sort`, `strings`, `sync`,
`time`, `context`, `encoding/json`, `fmt`, `net/http`,
`github.com/codewandler/llm`, `github.com/codewandler/llm/catalog`,
`github.com/codewandler/llm/tokencount`, `github.com/codewandler/llm/usage`).

---

## Step 2 — Update `Provider` struct

**Before:**
```go
type Provider struct {
	auth         *Auth
	opts         *llm.Options
	client       *http.Client
	defaultModel string
	modelOnce    sync.Once
	models       llm.Models
}
```

**After:**
```go
type Provider struct {
	auth         *Auth
	opts         *llm.Options
	core         *providercore.Client
	httpClient   *http.Client
	defaultModel string
	modelOnce    sync.Once
	models       llm.Models
}
```

`client *http.Client` (the transport-wrapped client) is replaced by:
- `core *providercore.Client` — owns all streaming HTTP
- `httpClient *http.Client` — plain client reused only by `FetchModels`

---

## Step 3 — Update `New()`

**Before:**
```go
func New(auth *Auth, opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	baseClient := cfg.HTTPClient
	if baseClient == nil {
		baseClient = llm.DefaultHttpClient()
	}
	clientCopy := *baseClient
	baseTransport := clientCopy.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	clientCopy.Transport = &transport{base: baseTransport, auth: auth}
	return &Provider{
		auth:         auth,
		opts:         cfg,
		client:       &clientCopy,
		defaultModel: DefaultModelID(),
	}
}
```

**After:**
```go
func New(auth *Auth, opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = llm.DefaultHttpClient()
	}
	p := &Provider{
		auth:         auth,
		opts:         cfg,
		httpClient:   httpClient,
		defaultModel: DefaultModelID(),
	}
	p.core = p.buildCore()
	return p
}
```

---

## Step 4 — Update `FetchModels()`

One-line change: `p.client.Do(req)` → `p.httpClient.Do(req)`.

**Before:**
```go
resp, err := p.client.Do(req)
```

**After:**
```go
resp, err := p.httpClient.Do(req)
```

Everything else in `FetchModels` stays identical.

---

## Step 5 — Replace `CreateStream()`

**Before** (entire 55-line method):
```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	reqOpts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}
	if reqOpts.Model == "" || reqOpts.Model == llm.ModelDefault {
		reqOpts.Model = p.defaultModel
	}
	enriched, err := openai.EnrichRequest(reqOpts)
	if err != nil {
		return nil, err
	}
	body, err := openai.BuildResponsesBody(enriched)
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.opts.BaseURL, "/")+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}
	req.Header.Set("Content-Type", "application/json")

	pub, ch := llm.NewEventPublisher()
	pub.Publish(&llm.RequestEvent{
		OriginalRequest: reqOpts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: llm.ApiTypeOpenAIResponses,
	})
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{Model: reqOpts.Model, Messages: reqOpts.Messages, Tools: reqOpts.Tools}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, p.Name(), reqOpts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	startTime := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(p.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		pub.Close()
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(errBody))
	}

	go openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
		RequestedModel: reqOpts.Model,
		StartTime:      startTime,
		ProviderName:   p.Name(),
		Logger:         p.opts.Logger,
	})
	return ch, nil
}
```

**After:**
```go
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.core.Stream(ctx, src)
}
```

---

## Step 6 — Add `buildCore()` and `coreOptions()`

These two new methods replace the deleted `transport` struct and
`injectBodyFields` function.

```go
func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName: llm.ProviderNameCodex,
		DefaultModel: p.defaultModel,
		BaseURL:      defaultBaseURL,
		// BasePath routes directly to the Codex endpoint, replacing the
		// old transport URL rewrite (/v1/responses → /codex/responses).
		BasePath: "/codex/responses",
		APIHint:  llm.ApiTypeOpenAIResponses,

		// HeaderFunc replaces transport.RoundTrip's auth-header injection.
		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			token, err := p.auth.Token(ctx)
			if err != nil {
				return nil, fmt.Errorf("codex: get token: %w", err)
			}
			return http.Header{
				"Authorization": {"Bearer " + token},
				accountIDHeader: {p.auth.AccountID()},
				codexBetaHeader: {codexBetaValue},
				"originator":    {codexOriginator},
			}, nil
		},

		// MutateRequest replaces injectBodyFields:
		//   - sets "store": false  (prevents Responses API persisting conversations)
		//   - removes max_tokens, max_output_tokens, prompt_cache_retention
		//
		// Note: responses.Request.Store is `bool` with `json:"store,omitempty"`,
		// so false cannot be emitted via the typed struct. Raw JSON mutation is
		// required here; TransformWireRequest cannot be used for this field.
		MutateRequest: func(r *http.Request) {
			if r.Body == nil || r.Header.Get("Content-Type") != "application/json" {
				return
			}
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			payload["store"] = false
			delete(payload, "max_tokens")
			delete(payload, "max_output_tokens")
			delete(payload, "prompt_cache_retention")
			encoded, err := json.Marshal(payload)
			if err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
		},

		// PreprocessRequest handles the codex-specific effort mapping that was
		// previously performed by openai.EnrichRequest inside CreateStream.
		// All codex models support "xhigh" effort (EffortMax); they cannot
		// reliably disable reasoning, so ThinkingOff clears the effort field.
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			switch {
			case req.Thinking.IsOff():
				// Codex models don't support disabling reasoning — omit effort.
				req.Effort = llm.EffortUnspecified
			case req.Effort == llm.EffortMax:
				// EffortMax maps to "xhigh" for codex models.
				req.Effort = llm.Effort("xhigh")
			}
			return req, original, nil
		},

		TokenCounter: tokencount.TokenCounterFunc(p.CountTokens),
	}
	return providercore.New(cfg, p.coreOptions()...)
}

func (p *Provider) coreOptions() []llm.Option {
	if p.opts == nil {
		return nil
	}
	var opts []llm.Option
	if p.opts.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(p.opts.BaseURL))
	}
	if p.opts.HTTPClient != nil {
		opts = append(opts, llm.WithHTTPClient(p.opts.HTTPClient))
	}
	if p.opts.Logger != nil {
		opts = append(opts, llm.WithLogger(p.opts.Logger))
	}
	return opts
}
```

---

## Step 7 — Delete dead code

Remove entirely from `provider.go`:

```go
// DELETE: transport struct and its RoundTrip method
type transport struct {
	base http.RoundTripper
	auth *Auth
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) { ... }

// DELETE: injectBodyFields
func injectBodyFields(body io.ReadCloser) (io.ReadCloser, int64, error) { ... }
```

---

## Complete resulting `provider.go`

```go
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/catalog"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const defaultBaseURL = "https://chatgpt.com/backend-api"

type Provider struct {
	auth         *Auth
	opts         *llm.Options
	core         *providercore.Client
	httpClient   *http.Client
	defaultModel string
	modelOnce    sync.Once
	models       llm.Models
}

func DefaultOptions() []llm.Option {
	return []llm.Option{llm.WithBaseURL(defaultBaseURL)}
}

func New(auth *Auth, opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = llm.DefaultHttpClient()
	}
	p := &Provider{
		auth:         auth,
		opts:         cfg,
		httpClient:   httpClient,
		defaultModel: DefaultModelID(),
	}
	p.core = p.buildCore()
	return p
}

func (p *Provider) Name() string { return llm.ProviderNameCodex }

func (*Provider) CostCalculator() usage.CostCalculator { return usage.Default() }

func (p *Provider) Resolve(modelID string) (llm.Model, error) { return p.Models().Resolve(modelID) }

func (p *Provider) Models() llm.Models {
	p.modelOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		resolved, err := llm.ResolveCatalog(ctx, catalog.RegisteredSource{
			Stage:     catalog.StageRuntime,
			Authority: catalog.AuthorityLocal,
			Source:    NewRuntimeSource(),
		})
		if err == nil {
			models := llm.CatalogModelsForRuntime(resolved, runtimeID, false, llm.CatalogModelProjectionOptions{
				ProviderName:          p.Name(),
				ExcludeBuiltinAliases: true,
			})
			if len(models) > 0 {
				p.models = attachProviderAliases(models)
				return
			}
		}
		p.models = fallbackModels()
	})
	return p.models
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	token, err := p.auth.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("codex: get token: %w", err)
	}
	endpoint := strings.TrimRight(p.opts.BaseURL, "/") + "/codex/models?client_version=" + modelsClientVersion
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("codex: create models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(accountIDHeader, p.auth.AccountID())
	req.Header.Set(codexBetaHeader, codexBetaValue)
	req.Header.Set("originator", codexOriginator)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codex list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(body))
	}
	var payload modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]llm.Model, 0, len(payload.Models))
	for _, model := range payload.Models {
		models = append(models, llm.Model{
			ID:       model.Slug,
			Name:     firstNonEmpty(model.DisplayName, model.Slug),
			Provider: p.Name(),
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.core.Stream(ctx, src)
}

func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName: llm.ProviderNameCodex,
		DefaultModel: p.defaultModel,
		BaseURL:      defaultBaseURL,
		BasePath:     "/codex/responses",
		APIHint:      llm.ApiTypeOpenAIResponses,

		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			token, err := p.auth.Token(ctx)
			if err != nil {
				return nil, fmt.Errorf("codex: get token: %w", err)
			}
			return http.Header{
				"Authorization": {"Bearer " + token},
				accountIDHeader: {p.auth.AccountID()},
				codexBetaHeader: {codexBetaValue},
				"originator":    {codexOriginator},
			}, nil
		},

		MutateRequest: func(r *http.Request) {
			if r.Body == nil || r.Header.Get("Content-Type") != "application/json" {
				return
			}
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			payload["store"] = false
			delete(payload, "max_tokens")
			delete(payload, "max_output_tokens")
			delete(payload, "prompt_cache_retention")
			encoded, err := json.Marshal(payload)
			if err != nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(encoded))
			r.ContentLength = int64(len(encoded))
		},

		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			original := req.Model
			switch {
			case req.Thinking.IsOff():
				req.Effort = llm.EffortUnspecified
			case req.Effort == llm.EffortMax:
				req.Effort = llm.Effort("xhigh")
			}
			return req, original, nil
		},

		TokenCounter: tokencount.TokenCounterFunc(p.CountTokens),
	}
	return providercore.New(cfg, p.coreOptions()...)
}

func (p *Provider) coreOptions() []llm.Option {
	if p.opts == nil {
		return nil
	}
	var opts []llm.Option
	if p.opts.BaseURL != "" {
		opts = append(opts, llm.WithBaseURL(p.opts.BaseURL))
	}
	if p.opts.HTTPClient != nil {
		opts = append(opts, llm.WithHTTPClient(p.opts.HTTPClient))
	}
	if p.opts.Logger != nil {
		opts = append(opts, llm.WithLogger(p.opts.Logger))
	}
	return opts
}
```

---

## Verification

After applying all steps (and after the stream_bridge prerequisite is fixed):

```
go build ./provider/codex/...
go vet ./provider/codex/...
go test -v -run 'TestCodex.*' ./provider/codex/ -timeout 120s
```

All five integration tests must stay green.
