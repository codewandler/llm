package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/internal/sse"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
)

const (
	defaultBaseURL = "https://openrouter.ai/api"
	providerName   = "openrouter"

	// DefaultModel is the recommended default model for OpenRouter.
	DefaultModel = "auto"
)

// Provider implements the OpenRouter LLM backend.
type Provider struct {
	opts         *llm.Options
	defaultModel string
	client       *http.Client
	models       llm.Models
}

// DefaultOptions returns the default options for OpenRouter.
// Base URL defaults to https://openrouter.ai/api.
// API key should be provided via WithAPIKey() or WithAPIKeyFunc().
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
	}
}

// New creates a new OpenRouter provider.
// Options are applied on top of DefaultOptions().
//
// Example usage:
//
//	// Add API key
//	p := openrouter.New(llm.WithAPIKey("sk-or-..."))
//
//	// Add API key from environment
//	p := openrouter.New(llm.APIKeyFromEnv("OPENROUTER_API_KEY"))
//
//	// Add dynamic API key resolution
//	p := openrouter.New(llm.WithAPIKeyFunc(func(ctx context.Context) (string, error) {
//	    return secretStore.Get(ctx, "openrouter-key")
//	}))
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	return &Provider{
		opts:         cfg,
		defaultModel: DefaultModel,
		client:       client,
		models:       loadEmbeddedModels(),
	}
}

// WithDefaultModel sets the default model to use.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	return p
}

// DefaultModel returns the configured default model ToolCallID.
func (p *Provider) DefaultModel() string { return p.defaultModel }
func (p *Provider) Name() string         { return providerName }
func (p *Provider) Models() llm.Models   { return p.models }
func (p *Provider) Resolve(model string) (llm.Model, error) {
	return p.models.Resolve(p.normalizeRequestModel(model))
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.opts.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOpenRouter, resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]llm.Model, len(result.Data))
	for i, m := range result.Data {
		models[i] = llm.Model{
			ID:       m.ID,
			Name:     m.Name,
			Provider: providerName,
		}
	}
	return models, nil
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	requestedModel := opts.Model // save before normalisation
	opts.Model = p.normalizeRequestModel(opts.Model)
	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameOpenRouter)
	}
	if apiKey == "" {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameOpenRouter)
	}

	body, err := buildRequest(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}

	// Build http.Request first so URL, method, and headers are available for
	// the RequestEvent. The request is fully constructed here but not yet sent.
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenRouter, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	// Create publisher and emit RequestEvent BEFORE the HTTP call.
	pub, ch := llm.NewEventPublisher()

	// Emit model resolution when the client-side normalisation changed the name
	// (e.g. "" or ModelDefault → "auto").
	if opts.Model != requestedModel {
		pub.ModelResolved(providerName, requestedModel, opts.Model)
	}

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(httpReq, body),
	})

	resp, err := p.client.Do(httpReq)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(llm.ProviderNameOpenRouter, err)
	}

	if resp.StatusCode != http.StatusOK {
		pub.Close()
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOpenRouter, resp.StatusCode, string(errBody))
	}

	go parseStream(ctx, resp.Body, pub, opts.Model)
	return ch, nil
}

func (p *Provider) normalizeRequestModel(model string) string {
	switch model {
	case "", llm.ModelDefault:
		return p.defaultModel
	default:
		return model
	}
}

// --- Request building ---

type request struct {
	Model          string           `json:"model"`
	Messages       []messagePayload `json:"messages"`
	Tools          []toolPayload    `json:"tools,omitempty"`
	ToolChoice     any              `json:"tool_choice,omitempty"`
	Reasoning      *reasoningConfig `json:"reasoning,omitempty"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Temperature    float64          `json:"temperature,omitempty"`
	TopP           float64          `json:"top_p,omitempty"`
	TopK           int              `json:"top_k,omitempty"`
	ResponseFormat *respFormat      `json:"response_format,omitempty"`
	Stream         bool             `json:"stream"`
	StreamOptions  *streamOptions   `json:"stream_options,omitempty"`
}

type reasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type respFormat struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type reasoningDetailInput struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type messagePayload struct {
	Role             string                 `json:"role"`
	Content          string                 `json:"content,omitempty"`
	Reasoning        string                 `json:"reasoning,omitempty"`
	ReasoningDetails []reasoningDetailInput `json:"reasoning_details,omitempty"`
	ToolCalls        []toolCallItem         `json:"tool_calls,omitempty"`
	ToolCallID       string                 `json:"tool_call_id,omitempty"`
}

type toolCallItem struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolPayload struct {
	Type     string          `json:"type"`
	Function functionPayload `json:"function"`
}

type functionPayload struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func buildRequest(opts llm.Request) ([]byte, error) {
	r := request{
		Model:         opts.Model,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
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
	if opts.TopK > 0 {
		r.TopK = opts.TopK
	}
	if opts.OutputFormat == llm.OutputFormatJSON {
		r.ResponseFormat = &respFormat{Type: "json_object"}
	}
	effort := opts.Effort
	if effort == llm.EffortMax {
		effort = llm.EffortHigh
	}
	if opts.Thinking.IsOn() && effort.IsEmpty() {
		effort = llm.EffortHigh
	}
	// ThinkingOff: omit reasoning block entirely (sending it enables reasoning).
	if !effort.IsEmpty() && !opts.Thinking.IsOff() {
		r.Reasoning = &reasoningConfig{Effort: string(effort)}
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{
			Type: "function",
			Function: functionPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	if len(opts.Tools) > 0 {
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = "auto"
		case llm.ToolChoiceRequired:
			r.ToolChoice = "required"
		case llm.ToolChoiceNone:
			r.ToolChoice = "none"
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{
				"type": "function",
				"function": map[string]string{
					"name": tc.Name,
				},
			}
		}
	}

	for _, m := range opts.Messages {
		switch m.Role {
		case msg.RoleSystem:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "system",
				Content: m.Text(),
			})
		case msg.RoleUser:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "user",
				Content: m.Text(),
			})
		case msg.RoleAssistant:
			mp := messagePayload{
				Role:    "assistant",
				Content: m.Text(),
			}
			thinkingParts := m.Parts.ByType(msg.PartTypeThinking)
			if len(thinkingParts) > 0 {
				var reasoningText strings.Builder
				for _, part := range thinkingParts {
					if part.Thinking == nil {
						continue
					}
					reasoningText.WriteString(part.Thinking.Text)
					mp.ReasoningDetails = append(mp.ReasoningDetails, reasoningDetailInput{
						Type:      "reasoning.text",
						Text:      part.Thinking.Text,
						Signature: part.Thinking.Signature,
					})
				}
				mp.Reasoning = reasoningText.String()
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, _ := json.Marshal(tc.Args)
				mp.ToolCalls = append(mp.ToolCalls, toolCallItem{
					ID:   tc.ID,
					Type: "function",
					Function: functionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			r.Messages = append(r.Messages, mp)
		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Messages = append(r.Messages, messagePayload{
					Role:       "tool",
					Content:    tr.ToolOutput,
					ToolCallID: tr.ToolCallID,
				})
			}
		}
	}

	body, err := json.Marshal(r)
	return body, err
}

// --- SSE stream parsing ---

type streamReasoningDetail struct {
	Type      string  `json:"type"`
	Text      string  `json:"text,omitempty"`
	Signature string  `json:"signature,omitempty"`
	Index     *uint32 `json:"index,omitempty"`
}

type streamChunk struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content          string                  `json:"content,omitempty"`
			ReasoningContent string                  `json:"reasoning_content,omitempty"`
			ReasoningDetails []streamReasoningDetail `json:"reasoning_details,omitempty"`
			ToolCalls        []streamToolDelta       `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int     `json:"prompt_tokens"`
		CompletionTokens    int     `json:"completion_tokens"`
		TotalTokens         int     `json:"total_tokens"`
		Cost                float64 `json:"cost"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamToolDelta struct {
	Index    uint32 `json:"index"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`
}

type toolAccum struct {
	id      string
	name    string
	argsBuf strings.Builder
}

func parseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, requestedModel string) {
	defer pub.Close()

	activeTools := make(map[uint32]*toolAccum)
	startEmitted := false
	var usage *llm.Usage
	var stopReason llm.StopReason

	err := sse.ForEachDataLine(ctx, body, func(ev sse.Event) bool {
		data := ev.Data
		if data == "" {
			return true
		}

		if data == "[DONE]" {
			if usage != nil {
				pub.Usage(*usage)
			}
			pub.Completed(llm.CompletedEvent{StopReason: stopReason})
			return false
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			pub.Error(llm.NewErrStreamDecode(llm.ProviderNameOpenRouter, err))
			return true
		}

		if chunk.Error != nil {
			pub.Error(llm.NewErrProviderMsg(llm.ProviderNameOpenRouter, chunk.Error.Message))
			return false
		}

		if !startEmitted {
			startEmitted = true
			// If the API resolved a different model than was requested (e.g. "auto"
			// resolved to a specific model), emit ModelResolvedEvent first.
			if chunk.Model != "" && chunk.Model != requestedModel {
				pub.ModelResolved(providerName, requestedModel, chunk.Model)
			}
			pub.Started(llm.StreamStartedEvent{
				Model:     chunk.Model,
				RequestID: chunk.ID,
			})
		}

		if chunk.Usage != nil {
			usage = &llm.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
				Cost:         chunk.Usage.Cost,
			}
			if chunk.Usage.PromptTokensDetails != nil {
				usage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if chunk.Usage.CompletionTokensDetails != nil {
				usage.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}

		if len(chunk.Choices) >= 1 {
			choice := chunk.Choices[0]
			if choice.Delta.ReasoningContent != "" {
				pub.Delta(llm.ThinkingDelta(choice.Delta.ReasoningContent))
			}
			for _, detail := range choice.Delta.ReasoningDetails {
				if detail.Text == "" {
					continue
				}
				delta := llm.ThinkingDelta(detail.Text)
				delta.Index = detail.Index
				pub.Delta(delta)
			}
			if choice.Delta.Content != "" {
				pub.Delta(llm.TextDelta(choice.Delta.Content))
			}
			for _, tc := range choice.Delta.ToolCalls {
				accum, ok := activeTools[tc.Index]
				if !ok {
					accum = &toolAccum{}
					activeTools[tc.Index] = accum
				}
				if tc.ID != "" {
					accum.id = tc.ID
				}
				if tc.Function.Name != "" {
					accum.name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					accum.argsBuf.WriteString(tc.Function.Arguments)
					d := llm.ToolDelta(accum.id, accum.name, tc.Function.Arguments)
					d.Index = &tc.Index
					pub.Delta(d)
				}
			}
			if choice.FinishReason != nil {
				stopReason = finishReasonToStopReason(*choice.FinishReason)
				if *choice.FinishReason == "tool_calls" {
					emitToolCalls(activeTools, pub)
				}
			}
		}

		return true
	})
	if err != nil {
		if ctx.Err() != nil {
			pub.Error(llm.NewErrContextCancelled(llm.ProviderNameOpenRouter, err))
			return
		}
		pub.Error(llm.NewErrStreamRead(llm.ProviderNameOpenRouter, err))
	}
}

func emitToolCalls(activeTools map[uint32]*toolAccum, pub llm.Publisher) {
	if len(activeTools) == 0 {
		return
	}
	indices := make([]uint32, 0, len(activeTools))
	for idx := range activeTools {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	for _, idx := range indices {
		accum := activeTools[idx]
		var args map[string]any
		if accum.argsBuf.Len() > 0 {
			_ = json.Unmarshal([]byte(accum.argsBuf.String()), &args)
		}
		pub.ToolCall(tool.NewToolCall(accum.id, accum.name, args))
		delete(activeTools, idx)
	}
}

// finishReasonToStopReason converts an OpenAI-compatible finish_reason string to a
// typed StopReason.
func finishReasonToStopReason(finishReason string) llm.StopReason {
	switch finishReason {
	case "stop":
		return llm.StopReasonEndTurn
	case "tool_calls":
		return llm.StopReasonToolUse
	case "length":
		return llm.StopReasonMaxTokens
	case "content_filter":
		return llm.StopReasonContentFilter
	default:
		return llm.StopReason(finishReason)
	}
}
