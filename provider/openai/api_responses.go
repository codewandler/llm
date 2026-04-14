package openai

// Responses API (/v1/responses) implementation.
//
// This endpoint is required for Codex models (gpt-5.x-codex,
// gpt-5.1-codex-mini) which cannot be called via /v1/chat/completions.
//
// The Responses API differs from Chat Completions in several key ways:
//   - Input uses "input" array of items, not "messages"
//   - System prompt is expressed as top-level "instructions" or role "developer"
//   - Tool calls and tool results are separate items in the input array
//   - SSE event types are named differently (response.output_text.delta, etc.)
//   - Usage arrives in the terminal response.completed event
//   - No [DONE] sentinel; response.completed is the terminal event

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// streamResponses sends a Responses API request and returns an event channel.
// It is called by Provider.Publisher for Codex models.
func (p *Provider) streamResponses(ctx context.Context, opts llm.Request) (llm.Stream, error) {
	apiKey, err := p.opts.APIKeyFunc(ctx)
	if err != nil {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameOpenAI)
	}

	body, err := respBuildRequest(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenAI, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenAI, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	startTime := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameOpenAI, err)
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOpenAI, resp.StatusCode, string(errBody))
	}

	pub, ch := llm.NewEventPublisher()

	// Emit token estimates (primary + per-segment breakdown)
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, p.Name(), opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	go respParseStream(ctx, resp.Body, pub, respStreamMeta{
		requestedModel: opts.Model,
		startTime:      startTime,
		providerName:   p.Name(),
		logger:         p.opts.Logger,
	})
	return ch, nil
}

// --- Request building ---

// respRequest is the top-level JSON body sent to /v1/responses.
type respRequest struct {
	Model                string              `json:"model"`
	Input                []respInput         `json:"input"`
	Instructions         string              `json:"instructions,omitempty"`
	Tools                []respTool          `json:"tools,omitempty"`
	ToolChoice           any                 `json:"tool_choice,omitempty"`
	Reasoning            *respReason         `json:"reasoning,omitempty"`
	MaxOutputTokens      int                 `json:"max_output_tokens,omitempty"`
	Temperature          float64             `json:"temperature,omitempty"`
	TopP                 float64             `json:"top_p,omitempty"`
	TopK                 int                 `json:"top_k,omitempty"`
	ResponseFormat       *respResponseFormat `json:"response_format,omitempty"`
	PromptCacheRetention string              `json:"prompt_cache_retention,omitempty"`
	Stream               bool                `json:"stream"`
}

type respResponseFormat struct {
	Type string `json:"type"`
}

// respReason holds the reasoning configuration for reasoning models.
type respReason struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// respInput is a polymorphic input item in the "input" array.
//
// Supported types:
//   - (omitted / role-based): user/assistant/developer messages
//   - "function_call": assistant tool call
//   - "function_call_output": tool result
type respInput struct {
	// For message items — type is omitted, role is set.
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`

	// For function_call and function_call_output items.
	Type string `json:"type,omitempty"`

	// function_call fields
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// function_call_output fields
	Output string `json:"output,omitempty"`
}

// respTool is a tool definition in the Responses API format.
// Unlike Chat Completions, name/description/parameters are at the top level.
type respTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

func respBuildRequest(opts llm.Request) ([]byte, error) {
	r := respRequest{
		Model:  opts.Model,
		Stream: true,
	}

	// Extended prompt cache retention (24h) for supported models.
	// Without this the Responses API defaults to ~5 minutes, which is too
	// short for multi-turn agent sessions and produces near-zero cache hits.
	// Note: Codex-category models also use streamResponses but route to a
	// different backend that does not support this parameter — use
	// wantsExtendedCacheInResponsesAPI (checks UseResponsesAPI flag) rather
	// than wantsExtendedCache (checks SupportsExtendedCache only).
	if wantsExtendedCacheInResponsesAPI(opts) {
		r.PromptCacheRetention = "24h"
	}

	// Generation parameters
	if opts.MaxTokens > 0 {
		r.MaxOutputTokens = opts.MaxTokens
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
		r.ResponseFormat = &respResponseFormat{Type: "json_object"}
	}

	// Build input items from messages.
	// The first SystemMsg becomes the top-level "instructions" field.
	// Subsequent SystemMsg entries become "developer" role items.
	instructionsSet := false
	for _, m := range opts.Messages {
		switch m.Role {
		case msg.RoleSystem:
			if !instructionsSet {
				r.Instructions = m.Text()
				instructionsSet = true
			} else {
				r.Input = append(r.Input, respInput{
					Role:    "developer",
					Content: m.Text(),
				})
			}

		case msg.RoleUser:
			r.Input = append(r.Input, respInput{
				Role:    "user",
				Content: m.Text(),
			})

		case msg.RoleAssistant:
			if m.Text() != "" {
				r.Input = append(r.Input, respInput{
					Role:    "assistant",
					Content: m.Text(),
				})
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, fmt.Errorf("marshal tool call arguments: %w", err)
				}
				r.Input = append(r.Input, respInput{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: string(argsJSON),
				})
			}

		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Input = append(r.Input, respInput{
					Type:   "function_call_output",
					CallID: tr.ToolCallID,
					Output: tr.ToolOutput,
				})
			}
		}
	}

	// Tools
	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, respTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sortmap.NewSortedMap(t.Parameters),
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
				"name": tc.Name,
			}
		}
	}

	// Reasoning effort
	if !opts.Effort.IsEmpty() {
		r.Reasoning = &respReason{
			Effort: string(opts.Effort),
		}
	}

	return json.Marshal(r)
}

// --- SSE stream parsing ---

// respStreamMeta is an alias for ccStreamMeta (defined in api_completions.go).
// Both parsers share the same per-request metadata shape.
type respStreamMeta = ccStreamMeta

// The Responses API emits these SSE event types (in the "event:" line):
//
//	response.created                       - stream opened
//	response.output_item.added             - new output item started (text or function_call)
//	response.output_text.delta             - text chunk
//	response.function_call_arguments.delta - tool argument chunk
//	response.output_item.done              - output item finished
//	response.completed                     - final event with usage
//
// There is no [DONE] sentinel; response.completed is the terminal event.

type respResponseCreated struct {
	Response struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"response"`
}

type respOutputItemAdded struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		Type   string `json:"type"` // "message" or "function_call"
		ID     string `json:"id"`
		CallID string `json:"call_id"`
		Name   string `json:"name"`
	} `json:"item"`
}

type respTextDelta struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

type respFuncArgsDelta struct {
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

type respOutputItemDone struct {
	OutputIndex int `json:"output_index"`
	Item        struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
}

type respResponseCompleted struct {
	Response struct {
		ID                string `json:"id"`
		Model             string `json:"model"`
		Status            string `json:"status"` // "completed", "incomplete", "failed"
		IncompleteDetails *struct {
			Reason string `json:"reason"` // "max_output_tokens", "content_filter"
		} `json:"incomplete_details,omitempty"`
		Usage *struct {
			InputTokens        int `json:"input_tokens"`
			OutputTokens       int `json:"output_tokens"`
			InputTokensDetails *struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"input_tokens_details,omitempty"`
			OutputTokensDetails *struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"output_tokens_details,omitempty"`
		} `json:"usage,omitempty"`
	} `json:"response"`
}

// respToolAccum accumulates streaming function_call argument chunks.
type respToolAccum struct {
	id     string
	name   string
	argBuf strings.Builder
}

func respParseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta respStreamMeta) {
	defer pub.Close()

	activeTools := make(map[int]*respToolAccum)
	startEmitted := false
	hadToolCalls := false

	err := sse.ForEachDataLine(ctx, body, func(ev sse.Event) bool {
		respHandleEvent(ev.Name, ev.Data, &startEmitted, &hadToolCalls, activeTools, pub, &meta)
		return true
	})
	if err != nil {
		if ctx.Err() != nil {
			pub.Error(llm.NewErrContextCancelled(llm.ProviderNameOpenAI, err))
			return
		}
		pub.Error(llm.NewErrStreamRead(llm.ProviderNameOpenAI, err))
	}
}

func respHandleEvent(
	eventName, data string,
	startEmitted *bool,
	hadToolCalls *bool,
	activeTools map[int]*respToolAccum,
	pub llm.Publisher,
	meta *respStreamMeta,
) {
	switch eventName {
	case "response.created":
		var ev respResponseCreated
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		meta.responseID = ev.Response.ID
		meta.responseModel = ev.Response.Model

	case "response.reasoning_summary_text.delta":
		var ev respTextDelta
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if ev.Delta != "" {
			if !*startEmitted {
				*startEmitted = true
				pub.Started(llm.StreamStartedEvent{
					Model:     meta.responseModel,
					RequestID: meta.responseID,
				})
			}
			pub.Delta(llm.ThinkingDelta(ev.Delta).WithIndex(uint32(ev.OutputIndex)))
		}

	case "response.output_text.delta":
		var ev respTextDelta
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if ev.Delta == "" {
			return
		}
		if !*startEmitted {
			*startEmitted = true
			pub.Started(llm.StreamStartedEvent{
				Model:     meta.responseModel,
				RequestID: meta.responseID,
			})
		}
		pub.Delta(llm.TextDelta(ev.Delta).WithIndex(uint32(ev.OutputIndex)))

	case "response.output_item.added":
		var ev respOutputItemAdded
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if ev.Item.Type == "function_call" {
			if !*startEmitted {
				*startEmitted = true
				pub.Started(llm.StreamStartedEvent{
					Model:     meta.responseModel,
					RequestID: meta.responseID,
				})
			}
			activeTools[ev.OutputIndex] = &respToolAccum{
				id:   ev.Item.CallID,
				name: ev.Item.Name,
			}
		}

	case "response.function_call_arguments.delta":
		var ev respFuncArgsDelta
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if accum, ok := activeTools[ev.OutputIndex]; ok {
			accum.argBuf.WriteString(ev.Delta)
			pub.Delta(llm.ToolDelta(accum.id, accum.name, ev.Delta).WithIndex(uint32(ev.OutputIndex)))
		}

	case "response.output_item.done":
		var ev respOutputItemDone
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if ev.Item.Type != "function_call" {
			return
		}
		argStr := ev.Item.Arguments
		if argStr == "" {
			if accum, ok := activeTools[ev.OutputIndex]; ok {
				argStr = accum.argBuf.String()
			}
		}

		var args map[string]any
		if argStr != "" {
			_ = json.Unmarshal([]byte(argStr), &args)
		}

		callID, name := ev.Item.CallID, ev.Item.Name
		if accum, ok := activeTools[ev.OutputIndex]; ok {
			if callID == "" {
				callID = accum.id
			}
			if name == "" {
				name = accum.name
			}
			delete(activeTools, ev.OutputIndex)
		}

		pub.ToolCall(tool.NewToolCall(callID, name, args))
		*hadToolCalls = true

	case "response.completed":
		var ev respResponseCompleted
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			pub.Completed(llm.CompletedEvent{StopReason: llm.StopReasonEndTurn})
			return
		}

		if !*startEmitted {
			*startEmitted = true
			pub.Started(llm.StreamStartedEvent{
				Model:     ev.Response.Model,
				RequestID: ev.Response.ID,
			})
		}

		var usageRec *usage.Record
		if u := ev.Response.Usage; u != nil {
			cached := 0
			if u.InputTokensDetails != nil {
				cached = u.InputTokensDetails.CachedTokens
			}
			reasoningTok := 0
			if u.OutputTokensDetails != nil {
				reasoningTok = u.OutputTokensDetails.ReasoningTokens
			}
			items := buildUsageTokenItems(u.InputTokens, u.OutputTokens, cached, reasoningTok, meta.logger, meta.provider(), meta.requestedModel)
			rec := usage.Record{
				Dims:       usage.Dims{Provider: meta.provider(), Model: meta.requestedModel, RequestID: meta.responseID},
				Tokens:     items,
				RecordedAt: time.Now(),
			}
			if cost, ok := usage.Default().Calculate(meta.provider(), meta.requestedModel, items); ok {
				rec.Cost = cost
			}
			usageRec = &rec
		}

		stopReason := llm.StopReasonEndTurn
		if ev.Response.Status == "incomplete" && ev.Response.IncompleteDetails != nil {
			switch ev.Response.IncompleteDetails.Reason {
			case "max_output_tokens":
				stopReason = llm.StopReasonMaxTokens
			case "content_filter":
				stopReason = llm.StopReasonContentFilter
			}
		} else if *hadToolCalls {
			stopReason = llm.StopReasonToolUse
		}
		if usageRec != nil {
			pub.UsageRecord(*usageRec)
		}
		pub.Completed(llm.CompletedEvent{StopReason: stopReason})

	case "error":
		var payload struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		msg := "responses API stream error"
		if json.Unmarshal([]byte(data), &payload) == nil && payload.Error.Message != "" {
			msg = payload.Error.Message
			if payload.Error.Code != "" {
				msg = fmt.Sprintf("%s (code: %s)", msg, payload.Error.Code)
			}
		}
		pub.Error(llm.NewErrProviderMsg(llm.ProviderNameOpenAI, msg))
	}
}
