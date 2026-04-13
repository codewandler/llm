package llm

import "github.com/codewandler/llm/msg"

type RequestOption func(r *Request)

type RequestBuilder struct {
	req *Request
}

func (b *RequestBuilder) applyOptions(opts ...RequestOption) {
	for _, opt := range opts {
		opt(b.req)
	}
}

func (b *RequestBuilder) Build(opts ...RequestOption) (Request, error) {
	b.applyOptions(opts...)

	// TODO: b.req.normalize()

	if err := b.req.Validate(); err != nil {
		return Request{}, err
	}

	return *b.req, nil
}

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

func newDefaultRequest() *Request {
	return &Request{
		Temperature: 0.7,
		Effort:      EffortLow,
		Thinking:    ThinkingOff,
		CacheHint:   msg.NewCacheHint(),
		ToolChoice:  ToolChoiceAuto{},
	}
}

func NewRequestBuilder() *RequestBuilder { return &RequestBuilder{req: newDefaultRequest()} }

func BuildRequest(opts ...RequestOption) (Request, error) {
	return NewRequestBuilder().Build()
}
