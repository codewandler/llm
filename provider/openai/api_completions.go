package openai

// Chat Completions API (/v1/chat/completions) implementation.
//
// This works for all general-purpose OpenAI models (gpt-4o, gpt-4.1, gpt-5,
// o-series, etc.). Codex models require the Responses API instead — see
// api_responses.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/internal/sse"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// streamCompletions sends a Chat Completions request and returns an event
// channel. It is called by Provider.Publisher for non-Codex models.
func (p *Provider) streamCompletions(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	apiKey, err := p.opts.APIKeyFunc(ctx)
	if err != nil {
		return nil, llm.NewErrMissingAPIKey(p.Name())
	}

	body, err := buildCompletionsBodyUnified(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, llm.NewErrBuildRequest(p.Name(), err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	pub, ch := llm.NewEventPublisher()

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: llm.ApiTypeOpenAIChatCompletion,
	})

	// Emit token estimates (primary + per-segment breakdown)
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, p.Name(), opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	startTime := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		pub.Close() // discard buffered pre-request events; ch is GC'd
		return nil, llm.NewErrRequestFailed(p.Name(), err)
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		pub.Close() // discard buffered pre-request events; ch is GC'd
		return nil, llm.NewErrAPIError(p.Name(), resp.StatusCode, string(errBody))
	}

	go ccParseStream(ctx, resp.Body, pub, ccStreamMeta{
		requestedModel: opts.Model,
		startTime:      startTime,
		providerName:   p.Name(),
		logger:         p.opts.Logger,
	})
	return ch, nil
}

// --- Request building ---

type ccRequest struct {
	Model                string             `json:"model"`
	Messages             []ccMessagePayload `json:"messages"`
	Tools                []ccToolPayload    `json:"tools,omitempty"`
	ToolChoice           any                `json:"tool_choice,omitempty"`
	ReasoningEffort      string             `json:"reasoning_effort,omitempty"`
	PromptCacheRetention string             `json:"prompt_cache_retention,omitempty"`
	Metadata             map[string]any     `json:"metadata,omitempty"`
	User                 string             `json:"user,omitempty"`
	MaxTokens            int                `json:"max_tokens,omitempty"`
	Temperature          float64            `json:"temperature,omitempty"`
	TopP                 float64            `json:"top_p,omitempty"`
	TopK                 int                `json:"top_k,omitempty"`
	ResponseFormat       *ccResponseFormat  `json:"response_format,omitempty"`
	Stream               bool               `json:"stream"`
	StreamOptions        *ccStreamOptions   `json:"stream_options,omitempty"`
}

type ccResponseFormat struct {
	Type string `json:"type"`
}

type ccStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type ccMessagePayload struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCalls  []ccToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type ccToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function ccFunctionCall `json:"function"`
}

type ccFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ccToolPayload struct {
	Type     string            `json:"type"`
	Function ccFunctionPayload `json:"function"`
}

type ccFunctionPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

func ccBuildRequest(opts llm.Request) ([]byte, error) {
	r := ccRequest{
		Model:         opts.Model,
		Stream:        true,
		StreamOptions: &ccStreamOptions{IncludeUsage: true},
	}

	// Generation parameters
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
		r.ResponseFormat = &ccResponseFormat{Type: "json_object"}
	}

	// Tools
	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, ccToolPayload{
			Type: "function",
			Function: ccFunctionPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  sortmap.NewSortedMap(t.Parameters),
			},
		})
	}

	// Tool choice
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

	// Reasoning effort (already mapped/validated by enrichOpts).
	if !opts.Effort.IsEmpty() {
		r.ReasoningEffort = string(opts.Effort)
	}

	// Prompt cache retention: 24h for models that support it or when explicitly
	// requested via CacheHint.TTL == "1h".
	if wantsExtendedCache(opts) {
		r.PromptCacheRetention = "24h"
	}
	if opts.RequestMeta != nil {
		r.User = opts.RequestMeta.User
		r.Metadata = opts.RequestMeta.Clone().Metadata
	}

	// Messages
	for _, m := range opts.Messages {
		switch m.Role {
		case msg.RoleSystem:
			r.Messages = append(r.Messages, ccMessagePayload{
				Role:    "system",
				Content: m.Text(),
			})

		case msg.RoleUser:
			r.Messages = append(r.Messages, ccMessagePayload{
				Role:    "user",
				Content: m.Text(),
			})

		case msg.RoleAssistant:
			mp := ccMessagePayload{
				Role:    "assistant",
				Content: m.Text(),
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, _ := json.Marshal(tc.Args)
				mp.ToolCalls = append(mp.ToolCalls, ccToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: ccFunctionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			r.Messages = append(r.Messages, mp)

		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Messages = append(r.Messages, ccMessagePayload{
					Role:       "tool",
					Content:    tr.ToolOutput,
					ToolCallID: tr.ToolCallID,
				})
			}
		}
	}

	return json.Marshal(r)
}

// --- SSE stream parsing ---

type ccStreamMeta struct {
	requestedModel string
	startTime      time.Time
	// responseID and responseModel are set by the Responses API parser when
	// response.created arrives, and used when emitting StreamEventStart on
	// first actual content.
	responseID    string
	responseModel string
	// providerName is the billing/attribution provider for usage records.
	// Defaults to llm.ProviderNameOpenAI; set to llm.ProviderNameChatGPT
	// for requests routed through the ChatGPT Codex backend.
	providerName string
	// logger, when non-nil, receives warnings about anomalous API data
	// (e.g. negative token counts from arithmetic clamping).
	logger *slog.Logger
}

// provider returns the provider name for usage records.
// Falls back to llm.ProviderNameOpenAI when providerName is empty.
func (m *ccStreamMeta) provider() string {
	if m.providerName != "" {
		return m.providerName
	}
	return llm.ProviderNameOpenAI
}

type ccStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		TotalTokens         int `json:"total_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
		CompletionTokensDetails *struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
}

type ccToolAccum struct {
	id      string
	name    string
	argsBuf strings.Builder
}

func ccParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta ccStreamMeta) {
	defer pub.Close()

	activeTools := make(map[int]*ccToolAccum)
	var inputTokens, outputTokens, cachedTokens, reasoningTokens int
	var stopReason llm.StopReason
	startEmitted := false

	err := sse.ForEachDataLine(ctx, body, func(ev sse.Event) bool {
		data := ev.Data
		if data == "" {
			return true
		}

		if data == "[DONE]" {
			items := buildUsageTokenItems(inputTokens, outputTokens, cachedTokens, reasoningTokens, meta.logger, meta.provider(), meta.requestedModel)

			rec := usage.Record{
				Dims:       usage.Dims{Provider: meta.provider(), Model: meta.requestedModel, RequestID: meta.responseID},
				Tokens:     items,
				RecordedAt: time.Now(),
			}
			if cost, ok := usage.Default().Calculate(meta.provider(), meta.requestedModel, items); ok {
				rec.Cost = cost
			}
			pub.UsageRecord(rec)
			pub.Completed(llm.CompletedEvent{StopReason: stopReason})
			return false
		}

		var chunk ccStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return true
		}

		if !startEmitted {
			startEmitted = true
			pub.Started(llm.StreamStartedEvent{
				Model:     chunk.Model,
				RequestID: chunk.ID,
				Provider:  meta.provider(),
			})
		}

		if chunk.Usage != nil {
			inputTokens = chunk.Usage.PromptTokens
			outputTokens = chunk.Usage.CompletionTokens
			if chunk.Usage.PromptTokensDetails != nil {
				cachedTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if chunk.Usage.CompletionTokensDetails != nil {
				reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}

		if len(chunk.Choices) == 0 {
			return true
		}
		choice := chunk.Choices[0]

		for _, tc := range choice.Delta.ToolCalls {
			accum, ok := activeTools[tc.Index]
			if !ok {
				accum = &ccToolAccum{}
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
				pub.Delta(llm.ToolDelta(accum.id, accum.name, tc.Function.Arguments).WithIndex(uint32(tc.Index)))
			}
		}

		if choice.Delta.Content != "" {
			pub.Delta(llm.TextDelta(choice.Delta.Content))
		}

		if choice.FinishReason != nil {
			stopReason = mapOpenAIFinishReason(*choice.FinishReason)
			if *choice.FinishReason == "tool_calls" {
				ccEmitToolCalls(activeTools, pub)
			}
		}
		return true
	})
	if err != nil {
		if ctx.Err() != nil {
			pub.Error(llm.NewErrContextCancelled(meta.provider(), err))
			return
		}
		pub.Error(llm.NewErrStreamRead(meta.provider(), err))
	}
}

func ccEmitToolCalls(activeTools map[int]*ccToolAccum, pub llm.Publisher) {
	indices := make([]int, 0, len(activeTools))
	for idx := range activeTools {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		accum := activeTools[idx]
		var args map[string]any
		if accum.argsBuf.Len() > 0 {
			_ = json.Unmarshal([]byte(accum.argsBuf.String()), &args)
		}
		pub.ToolCall(tool.NewToolCall(accum.id, accum.name, args))
	}
}
