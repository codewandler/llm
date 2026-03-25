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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tool"
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
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOpenAI, resp.StatusCode, string(errBody))
	}

	pub, ch := llm.NewEventPublisher()
	go respParseStream(ctx, resp.Body, pub, respStreamMeta{
		requestedModel: opts.Model,
		startTime:      startTime,
	})
	return ch, nil
}

// --- Request building ---

// respRequest is the top-level JSON body sent to /v1/responses.
type respRequest struct {
	Model          string              `json:"model"`
	Input          []respInput         `json:"input"`
	Instructions   string              `json:"instructions,omitempty"`
	Tools          []respTool          `json:"tools,omitempty"`
	ToolChoice     any                 `json:"tool_choice,omitempty"`
	Reasoning      *respReason         `json:"reasoning,omitempty"`
	MaxTokens      int                 `json:"max_tokens,omitempty"`
	Temperature    float64             `json:"temperature,omitempty"`
	TopP           float64             `json:"top_p,omitempty"`
	TopK           int                 `json:"top_k,omitempty"`
	ResponseFormat *respResponseFormat `json:"response_format,omitempty"`
	Stream         bool                `json:"stream"`
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
		r.ResponseFormat = &respResponseFormat{Type: "json_object"}
	}

	// Build input items from messages.
	// The first SystemMsg becomes the top-level "instructions" field.
	// Subsequent SystemMsg entries become "developer" role items.
	instructionsSet := false
	for _, msg := range opts.Messages {
		switch m := msg.(type) {
		case llm.SystemMessage:
			if !instructionsSet {
				r.Instructions = m.Content()
				instructionsSet = true
			} else {
				r.Input = append(r.Input, respInput{
					Role:    "developer",
					Content: m.Content(),
				})
			}

		case llm.UserMessage:
			r.Input = append(r.Input, respInput{
				Role:    "user",
				Content: m.Content(),
			})

		case llm.AssistantMessage:
			if m.Content() != "" {
				r.Input = append(r.Input, respInput{
					Role:    "assistant",
					Content: m.Content(),
				})
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.ToolArgs())
				if err != nil {
					return nil, fmt.Errorf("marshal tool call arguments: %w", err)
				}
				r.Input = append(r.Input, respInput{
					Type:      "function_call",
					CallID:    tc.ToolCallID(),
					Name:      tc.ToolName(),
					Arguments: string(argsJSON),
				})
			}

		case llm.ToolMessage:
			r.Input = append(r.Input, respInput{
				Type:   "function_call_output",
				CallID: m.ToolCallID(),
				Output: m.ToolOutput(),
			})
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
	if opts.ReasoningEffort != "" {
		r.Reasoning = &respReason{
			Effort: string(opts.ReasoningEffort),
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
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	activeTools := make(map[int]*respToolAccum)
	startEmitted := false
	hadToolCalls := false
	var pendingEvent string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			pub.Error(llm.NewErrContextCancelled(llm.ProviderNameOpenAI, ctx.Err()))
			return
		default:
		}

		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			pendingEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			respHandleEvent(pendingEvent, data, &startEmitted, &hadToolCalls, activeTools, pub, &meta)
			pendingEvent = ""
		}
	}

	if err := scanner.Err(); err != nil {
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
			pub.Delta(llm.ReasoningDelta(ev.Delta).WithIndex(uint32(ev.OutputIndex)))
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

		var usage *llm.Usage
		if u := ev.Response.Usage; u != nil {
			usage = &llm.Usage{
				InputTokens:  u.InputTokens,
				OutputTokens: u.OutputTokens,
				TotalTokens:  u.InputTokens + u.OutputTokens,
			}
			if u.InputTokensDetails != nil {
				usage.CacheReadTokens = u.InputTokensDetails.CachedTokens
			}
			if u.OutputTokensDetails != nil {
				usage.ReasoningTokens = u.OutputTokensDetails.ReasoningTokens
			}
			calculateCost(meta.requestedModel, usage)
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
		if usage != nil {
			pub.Usage(*usage)
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
