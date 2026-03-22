package openai

// Chat Completions API (/v1/chat/completions) implementation.
//
// This works for all general-purpose OpenAI models (gpt-4o, gpt-4.1, gpt-5,
// o-series, etc.). Codex models require the Responses API instead — see
// api_responses.go.

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

// streamCompletions sends a Chat Completions request and returns an event
// channel. It is called by Provider.Stream for non-Codex models.
func (p *Provider) streamCompletions(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
	apiKey, err := p.opts.APIKeyFunc(ctx)
	if err != nil {
		return nil, fmt.Errorf("get API key: %w", err)
	}

	body, err := ccBuildRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	startTime := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("completions request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("completions API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	stream := llm.NewEventStream()
	go ccParseStream(ctx, resp.Body, stream, ccStreamMeta{
		requestedModel: opts.Model,
		startTime:      startTime,
	})
	return stream.C(), nil
}

// --- Request building ---

type ccRequest struct {
	Model                string             `json:"model"`
	Messages             []ccMessagePayload `json:"messages"`
	Tools                []ccToolPayload    `json:"tools,omitempty"`
	ToolChoice           any                `json:"tool_choice,omitempty"`
	ReasoningEffort      string             `json:"reasoning_effort,omitempty"`
	PromptCacheRetention string             `json:"prompt_cache_retention,omitempty"`
	Stream               bool               `json:"stream"`
	StreamOptions        *ccStreamOptions   `json:"stream_options,omitempty"`
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

func ccBuildRequest(opts llm.StreamRequest) ([]byte, error) {
	r := ccRequest{
		Model:         opts.Model,
		Stream:        true,
		StreamOptions: &ccStreamOptions{IncludeUsage: true},
	}

	// Tools
	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, ccToolPayload{
			Type: "function",
			Function: ccFunctionPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  llm.NewSortedMap(t.Parameters),
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

	// Reasoning effort (already mapped/validated by Provider.Stream).
	if opts.ReasoningEffort != "" {
		r.ReasoningEffort = string(opts.ReasoningEffort)
	}

	// Prompt cache retention: 24h for models that support it or when explicitly
	// requested via CacheHint.TTL == "1h".
	if wantsExtendedCache(opts) {
		r.PromptCacheRetention = "24h"
	}

	// Messages
	for _, msg := range opts.Messages {
		switch m := msg.(type) {
		case *llm.SystemMsg:
			r.Messages = append(r.Messages, ccMessagePayload{
				Role:    "system",
				Content: m.Content,
			})

		case *llm.UserMsg:
			r.Messages = append(r.Messages, ccMessagePayload{
				Role:    "user",
				Content: m.Content,
			})

		case *llm.AssistantMsg:
			mp := ccMessagePayload{
				Role:    "assistant",
				Content: m.Content,
			}
			for _, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
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

		case *llm.ToolCallResult:
			r.Messages = append(r.Messages, ccMessagePayload{
				Role:       "tool",
				Content:    m.Output,
				ToolCallID: m.ToolCallID,
			})
		}
	}

	return json.Marshal(r)
}

// --- SSE stream parsing ---

type ccStreamMeta struct {
	requestedModel string
	startTime      time.Time
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

func ccParseStream(ctx context.Context, body io.ReadCloser, events *llm.EventStream, meta ccStreamMeta) {
	defer events.Close()
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	activeTools := make(map[int]*ccToolAccum)
	var finalUsage *llm.Usage
	startEmitted := false

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			events.Send(llm.StreamEvent{Type: llm.StreamEventError, Error: ctx.Err()})
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			if finalUsage != nil {
				calculateCost(meta.requestedModel, finalUsage)
			}
			events.Send(llm.StreamEvent{Type: llm.StreamEventDone, Usage: finalUsage})
			return
		}

		var chunk ccStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Emit StreamEventStart on first chunk.
		if !startEmitted {
			startEmitted = true
			events.Send(llm.StreamEvent{
				Type: llm.StreamEventStart,
				Start: &llm.StreamStart{
					ModelRequested:    meta.requestedModel,
					ModelResolved:     meta.requestedModel,
					ModelProviderID:   chunk.Model,
					ProviderRequestID: chunk.ID,
					TimeToFirstToken:  time.Since(meta.startTime),
				},
			})
		}

		// Accumulate usage (arrives on the final chunk before [DONE]).
		if chunk.Usage != nil {
			finalUsage = &llm.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
			if chunk.Usage.PromptTokensDetails != nil {
				finalUsage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if chunk.Usage.CompletionTokensDetails != nil {
				finalUsage.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]

		// Accumulate tool call argument chunks.
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
			accum.argsBuf.WriteString(tc.Function.Arguments)
		}

		// Text delta.
		if choice.Delta.Content != "" {
			events.Send(llm.StreamEvent{Type: llm.StreamEventDelta, Delta: choice.Delta.Content})
		}

		// Emit completed tool calls on finish_reason == "tool_calls".
		if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
			ccEmitToolCalls(activeTools, events)
		}
	}

	if err := scanner.Err(); err != nil {
		events.Send(llm.StreamEvent{
			Type:  llm.StreamEventError,
			Error: fmt.Errorf("stream scan error: %w", err),
		})
	}
}

func ccEmitToolCalls(activeTools map[int]*ccToolAccum, events *llm.EventStream) {
	for _, accum := range activeTools {
		var args map[string]any
		if accum.argsBuf.Len() > 0 {
			_ = json.Unmarshal([]byte(accum.argsBuf.String()), &args)
		}
		events.Send(llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:        accum.id,
				Name:      accum.name,
				Arguments: args,
			},
		})
	}
}
