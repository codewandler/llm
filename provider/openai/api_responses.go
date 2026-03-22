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
)

// streamResponses sends a Responses API request and returns an event channel.
// It is called by Provider.Stream for Codex models.
func (p *Provider) streamResponses(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
	apiKey, err := p.opts.APIKeyFunc(ctx)
	if err != nil {
		return nil, fmt.Errorf("get API key: %w", err)
	}

	body, err := respBuildRequest(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOpenAI, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
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

	stream := llm.NewEventStream()
	go respParseStream(ctx, resp.Body, stream, respStreamMeta{
		requestedModel: opts.Model,
		startTime:      startTime,
	})
	return stream.C(), nil
}

// --- Request building ---

// respRequest is the top-level JSON body sent to /v1/responses.
type respRequest struct {
	Model        string      `json:"model"`
	Input        []respInput `json:"input"`
	Instructions string      `json:"instructions,omitempty"`
	Tools        []respTool  `json:"tools,omitempty"`
	ToolChoice   any         `json:"tool_choice,omitempty"`
	Reasoning    *respReason `json:"reasoning,omitempty"`
	Stream       bool        `json:"stream"`
}

// respReason holds the effort level for reasoning models.
type respReason struct {
	Effort string `json:"effort"`
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

func respBuildRequest(opts llm.StreamRequest) ([]byte, error) {
	r := respRequest{
		Model:  opts.Model,
		Stream: true,
	}

	// Build input items from messages.
	// The first SystemMsg becomes the top-level "instructions" field.
	// Subsequent SystemMsg entries become "developer" role items.
	instructionsSet := false
	for _, msg := range opts.Messages {
		switch m := msg.(type) {
		case *llm.SystemMsg:
			if !instructionsSet {
				r.Instructions = m.Content
				instructionsSet = true
			} else {
				r.Input = append(r.Input, respInput{
					Role:    "developer",
					Content: m.Content,
				})
			}

		case *llm.UserMsg:
			r.Input = append(r.Input, respInput{
				Role:    "user",
				Content: m.Content,
			})

		case *llm.AssistantMsg:
			if m.Content != "" {
				r.Input = append(r.Input, respInput{
					Role:    "assistant",
					Content: m.Content,
				})
			}
			// Each tool call becomes a separate function_call item.
			for _, tc := range m.ToolCalls {
				argsJSON, err := json.Marshal(tc.Arguments)
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

		case *llm.ToolCallResult:
			r.Input = append(r.Input, respInput{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: m.Output,
			})
		}
	}

	// Tools
	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, respTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  llm.NewSortedMap(t.Parameters),
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

	// Reasoning effort — Responses API wraps it in {"reasoning": {"effort": "..."}}
	if opts.ReasoningEffort != "" {
		r.Reasoning = &respReason{Effort: string(opts.ReasoningEffort)}
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
	Delta string `json:"delta"`
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
		ID    string `json:"id"`
		Model string `json:"model"`
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

func respParseStream(ctx context.Context, body io.ReadCloser, events *llm.EventStream, meta respStreamMeta) {
	defer events.Close()
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	activeTools := make(map[int]*respToolAccum) // keyed by output_index
	startEmitted := false
	var pendingEvent string

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			events.Error(llm.NewErrContextCancelled(llm.ProviderNameOpenAI, ctx.Err()))
			return
		default:
		}

		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			pendingEvent = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			respHandleEvent(pendingEvent, data, &startEmitted, activeTools, events, meta)
			pendingEvent = ""
		}
	}

	if err := scanner.Err(); err != nil {
		events.Error(llm.NewErrStreamRead(llm.ProviderNameOpenAI, err))
	}
}

func respHandleEvent(
	eventName, data string,
	startEmitted *bool,
	activeTools map[int]*respToolAccum,
	events *llm.EventStream,
	meta respStreamMeta,
) {
	switch eventName {
	case "response.created":
		var ev respResponseCreated
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if !*startEmitted {
			*startEmitted = true
			events.Send(llm.StreamEvent{
				Type: llm.StreamEventStart,
				Start: &llm.StreamStart{
					ModelRequested:    meta.requestedModel,
					ModelResolved:     meta.requestedModel,
					ModelProviderID:   ev.Response.Model,
					ProviderRequestID: ev.Response.ID,
					TimeToFirstToken:  time.Since(meta.startTime),
				},
			})
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
			events.Send(llm.StreamEvent{
				Type: llm.StreamEventStart,
				Start: &llm.StreamStart{
					ModelRequested:   meta.requestedModel,
					ModelResolved:    meta.requestedModel,
					TimeToFirstToken: time.Since(meta.startTime),
				},
			})
		}
		events.Send(llm.StreamEvent{Type: llm.StreamEventDelta, Delta: ev.Delta})

	case "response.output_item.added":
		var ev respOutputItemAdded
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if ev.Item.Type == "function_call" {
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
		}

	case "response.output_item.done":
		var ev respOutputItemDone
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return
		}
		if ev.Item.Type != "function_call" {
			return
		}
		// The done event contains the authoritative final arguments string.
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

		events.ToolCall(llm.ToolCall{
			ID:        callID,
			Name:      name,
			Arguments: args,
		})

	case "response.completed":
		var ev respResponseCompleted
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			events.Send(llm.StreamEvent{Type: llm.StreamEventDone})
			return
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
		events.Send(llm.StreamEvent{Type: llm.StreamEventDone, Usage: usage})

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
		events.Error(llm.NewErrProviderMsg(llm.ProviderNameOpenAI, msg))
	}
}
