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

## Known blocker — tool-name resolution

Before executing this migration, a bug in `api/unified/stream_bridge_responses.go`
must be fixed (outside this scope, noted here as a prerequisite).

**Root cause:** The OpenAI standard sends `"name"` inside
`response.function_call_arguments.done`. Codex does not — it sends `name` and
`call_id` only in `response.output_item.added` / `response.output_item.done`.

Raw events from the Codex API for a tool call:

```
event: response.output_item.added
data: {"item":{"id":"fc_...","type":"function_call","call_id":"call_...","name":"get_weather"}, ...}

event: response.function_call_arguments.delta
data: {"delta":"{\"loc","item_id":"fc_...", ...}   ← no name, no call_id

event: response.function_call_arguments.done
data: {"arguments":"{\"location\":\"Tokyo\"}","item_id":"fc_...", ...}  ← no name, no call_id

event: response.output_item.done
data: {"item":{"id":"fc_...","call_id":"call_...","name":"get_weather","arguments":"..."}, ...}
```

The unified bridge emits the `ToolCall` event in `FunctionCallArgumentsDoneEvent`
using `e.Name` — which is empty for Codex. The name must instead be carried
forward from `OutputItemAddedEvent` (where `item.name` is present).

The fix to `stream_bridge_responses.go` is to maintain a name-tracking map
(keyed by `output_index`) populated from `OutputItemAddedEvent` and consumed
in `FunctionCallArgumentsDoneEvent`. The rest of the migration is independent
of this fix and can proceed in parallel.

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
