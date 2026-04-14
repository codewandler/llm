package llm

import (
	"context"

	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

// Buildable is implemented by any value that can produce a Request for
// streaming. Callers may pass either a fully-constructed Request or a
// *RequestBuilder to CreateStream — both satisfy this interface.
type Buildable interface {
	BuildRequest(ctx context.Context) (Request, error)
}

// Compile-time interface satisfaction checks.
var _ Buildable = Request{}
var _ Buildable = (*RequestBuilder)(nil)

// BuildRequest implements Buildable. Returns the request as-is without
// re-validating — providers call Validate() themselves after receiving opts,
// so passing a Request skips one validation round-trip compared to passing
// a *RequestBuilder (whose Build() also calls Validate()).
func (r Request) BuildRequest(_ context.Context) (Request, error) {
	return r, nil
}

// BuildRequest implements Buildable. Calls Build() and returns the result.
func (b *RequestBuilder) BuildRequest(_ context.Context) (Request, error) {
	return b.Build()
}

type RequestOption func(r *Request)

type RequestBuilder struct {
	req *Request
}

// Apply applies functional options to the builder and returns b for chaining.
// Build(opts...) internally delegates to Apply, so both are interchangeable
// for the terminal options; Apply is preferred when options are pre-assembled.
func (b *RequestBuilder) Apply(opts ...RequestOption) *RequestBuilder {
	for _, opt := range opts {
		opt(b.req)
	}
	return b
}

func (b *RequestBuilder) Build(opts ...RequestOption) (Request, error) {
	b.Apply(opts...)

	// TODO: b.req.normalize()

	if err := b.req.Validate(); err != nil {
		return Request{}, err
	}

	return *b.req, nil
}

// --- Fluent setter methods ---

func (b *RequestBuilder) Model(modelID string) *RequestBuilder {
	b.req.Model = modelID
	return b
}

func (b *RequestBuilder) Thinking(mode ThinkingMode) *RequestBuilder {
	b.req.Thinking = mode
	return b
}

func (b *RequestBuilder) Effort(level Effort) *RequestBuilder {
	b.req.Effort = level
	return b
}

func (b *RequestBuilder) MaxTokens(maxTokens int) *RequestBuilder {
	b.req.MaxTokens = maxTokens
	return b
}

func (b *RequestBuilder) Temperature(temperature float64) *RequestBuilder {
	b.req.Temperature = temperature
	return b
}

// OutputFormat sets the output format of the response.
func (b *RequestBuilder) OutputFormat(format OutputFormat) *RequestBuilder {
	b.req.OutputFormat = format
	return b
}

// TopK sets the top-k parameter for sampling.
func (b *RequestBuilder) TopK(k int) *RequestBuilder {
	b.req.TopK = k
	return b
}

func (b *RequestBuilder) TopP(p float64) *RequestBuilder {
	b.req.TopP = p
	return b
}

func (b *RequestBuilder) Coding() *RequestBuilder {
	return b.Thinking(ThinkingOn).
		Effort(EffortHigh).
		Temperature(0.1).
		MaxTokens(16_000)
}

// buildMsg constructs a Message from a msg.Builder, applying cache hints only
// when opts are provided. msg.Builder.Cache() with no args still creates a
// non-nil CacheHint — this guard preserves nil semantics when cache is omitted.
func buildMsg(b *msg.Builder, cache []CacheOpt) Message {
	if len(cache) > 0 {
		b = b.Cache(cache...)
	}
	return b.Build()
}

// System appends a system message. Pass CacheTTL1h or CacheTTL5m to enable
// prompt caching for this message. Omitting cache leaves CacheHint nil.
func (b *RequestBuilder) System(text string, cache ...CacheOpt) *RequestBuilder {
	b.req.Messages = append(b.req.Messages, buildMsg(msg.System(text), cache))
	return b
}

// User appends a user message. Pass CacheTTL1h or CacheTTL5m to enable
// prompt caching for this message. Omitting cache leaves CacheHint nil.
func (b *RequestBuilder) User(text string, cache ...CacheOpt) *RequestBuilder {
	b.req.Messages = append(b.req.Messages, buildMsg(msg.User(text), cache))
	return b
}

// Append appends pre-built messages (assistant turns, tool results, etc.).
func (b *RequestBuilder) Append(msgs ...Message) *RequestBuilder {
	b.req.Messages = append(b.req.Messages, msgs...)
	return b
}

// Tools appends tool definitions to the request. Multiple calls accumulate;
// all tools registered this way are sent to the model together.
func (b *RequestBuilder) Tools(defs ...tool.Definition) *RequestBuilder {
	b.req.Tools = append(b.req.Tools, defs...)
	return b
}

// ToolChoice sets the tool selection strategy.
func (b *RequestBuilder) ToolChoice(tc ToolChoice) *RequestBuilder {
	b.req.ToolChoice = tc
	return b
}

// --- Functional option constructors (With* prefix) ---
//
// Each With* function returns a RequestOption that sets a single field.
// They compose with Apply and BuildRequest, and can be accumulated in
// []RequestOption slices for programmatic configuration.

func WithModel(model string) RequestOption {
	return func(r *Request) { r.Model = model }
}

func WithThinking(mode ThinkingMode) RequestOption {
	return func(r *Request) { r.Thinking = mode }
}

func WithEffort(level Effort) RequestOption {
	return func(r *Request) { r.Effort = level }
}

func WithMaxTokens(n int) RequestOption {
	return func(r *Request) { r.MaxTokens = n }
}

func WithTemperature(t float64) RequestOption {
	return func(r *Request) { r.Temperature = t }
}

func WithOutputFormat(f OutputFormat) RequestOption {
	return func(r *Request) { r.OutputFormat = f }
}

func WithTopK(k int) RequestOption {
	return func(r *Request) { r.TopK = k }
}

func WithTopP(p float64) RequestOption {
	return func(r *Request) { r.TopP = p }
}

// WithSystem appends a system message. Same cache nil-guard semantics as
// the fluent System method: omitting cache leaves CacheHint nil.
func WithSystem(text string, cache ...CacheOpt) RequestOption {
	return func(r *Request) {
		r.Messages = append(r.Messages, buildMsg(msg.System(text), cache))
	}
}

// WithUser appends a user message. Same cache nil-guard semantics as
// the fluent User method: omitting cache leaves CacheHint nil.
func WithUser(text string, cache ...CacheOpt) RequestOption {
	return func(r *Request) {
		r.Messages = append(r.Messages, buildMsg(msg.User(text), cache))
	}
}

// WithMessages appends pre-built messages (assistant turns, tool results, etc.).
func WithMessages(msgs ...Message) RequestOption {
	return func(r *Request) { r.Messages = append(r.Messages, msgs...) }
}

// WithTools appends tool definitions to the request. Multiple calls accumulate;
// all tools registered this way are sent to the model together.
func WithTools(defs ...tool.Definition) RequestOption {
	return func(r *Request) { r.Tools = append(r.Tools, defs...) }
}

// WithToolChoice sets the tool selection strategy.
func WithToolChoice(tc ToolChoice) RequestOption {
	return func(r *Request) { r.ToolChoice = tc }
}

// --- Constructors ---

// NewRequestBuilder returns a zero-value builder. All fields default to their
// provider-level defaults (zero values). Call Build() only after setting Model.
func NewRequestBuilder() *RequestBuilder { return &RequestBuilder{req: &Request{}} }

// BuildRequest is a convenience wrapper; opts are passed through to Build.
func BuildRequest(opts ...RequestOption) (Request, error) {
	return NewRequestBuilder().Build(opts...)
}
