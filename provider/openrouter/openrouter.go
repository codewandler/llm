package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/codewandler/llm"
)

const (
	baseURL      = "https://openrouter.ai/api"
	providerName = "openrouter"

	// DefaultModel is the recommended default model for OpenRouter
	DefaultModel = "anthropic/claude-sonnet-4.5"
)

// Provider implements the OpenRouter LLM backend.
type Provider struct {
	apiKey       string
	defaultModel string
	client       *http.Client
}

// New creates a new OpenRouter provider.
func New(apiKey string) *Provider {
	return &Provider{
		apiKey:       apiKey,
		defaultModel: DefaultModel,
		client:       &http.Client{},
	}
}

// WithDefaultModel sets the default model to use.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	return p
}

// DefaultModel returns the configured default model ID.
func (p *Provider) DefaultModel() string {
	return p.defaultModel
}

func (p *Provider) Name() string { return providerName }

// Models returns the curated list of tool-enabled models from the embedded models.json file.
// This includes 229 models from various providers that support tool calling.
func (p *Provider) Models() []llm.Model {
	return loadEmbeddedModels()
}

func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter models API failed (HTTP %d): %s", resp.StatusCode, string(body))
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

func (p *Provider) SendMessage(ctx context.Context, opts llm.SendOptions) (<-chan llm.StreamEvent, error) {
	body, err := buildRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openrouter API request failed (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go parseStream(ctx, resp.Body, events)
	return events, nil
}

// --- Request building ---

type request struct {
	Model            string           `json:"model"`
	Messages         []messagePayload `json:"messages"`
	Tools            []toolPayload    `json:"tools,omitempty"`
	Stream           bool             `json:"stream"`
	IncludeReasoning bool             `json:"include_reasoning,omitempty"`
}

type messagePayload struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []toolCallItem `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
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

func buildRequest(opts llm.SendOptions) ([]byte, error) {
	r := request{
		Model:            opts.Model,
		Stream:           true,
		IncludeReasoning: true,
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

	for _, msg := range opts.Messages {
		switch msg.Role {
		case llm.RoleSystem:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "system",
				Content: msg.Content,
			})

		case llm.RoleUser:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "user",
				Content: msg.Content,
			})

		case llm.RoleAssistant:
			m := messagePayload{
				Role:    "assistant",
				Content: msg.Content,
			}
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				m.ToolCalls = append(m.ToolCalls, toolCallItem{
					ID:   tc.ID,
					Type: "function",
					Function: functionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			r.Messages = append(r.Messages, m)

		case llm.RoleTool:
			content := msg.Content
			if content == "" {
				content = "<empty>"
			}
			r.Messages = append(r.Messages, messagePayload{
				Role:       "tool",
				Content:    content,
				ToolCallID: toolCallIDFromPreceding(opts.Messages, msg),
			})
		}
	}

	return json.Marshal(r)
}

// toolCallIDFromPreceding finds the tool_call_id for a tool result message
// by looking at the preceding assistant message's tool calls.
func toolCallIDFromPreceding(messages []llm.Message, toolMsg llm.Message) string {
	// Walk backwards from this tool message to find the assistant message,
	// then match by position.
	toolIdx := -1
	for i, m := range messages {
		if m.ID == toolMsg.ID {
			toolIdx = i
			break
		}
	}
	if toolIdx < 0 {
		return ""
	}

	// Count how many consecutive tool messages precede this one (to get position).
	pos := 0
	for j := toolIdx - 1; j >= 0; j-- {
		if messages[j].Role == llm.RoleTool {
			pos++
		} else {
			break
		}
	}

	// Find the preceding assistant message.
	for j := toolIdx - 1 - pos; j >= 0; j-- {
		if messages[j].Role == llm.RoleAssistant && pos < len(messages[j].ToolCalls) {
			return messages[j].ToolCalls[pos].ID
		}
	}
	return ""
}

// --- SSE stream parsing ---

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string            `json:"content,omitempty"`
			ReasoningContent string            `json:"reasoning_content,omitempty"`
			ToolCalls        []streamToolDelta `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type streamToolDelta struct {
	Index    int `json:"index"`
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

func parseStream(ctx context.Context, body io.ReadCloser, events chan<- llm.StreamEvent) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	activeTools := make(map[int]*toolAccum)
	doneSent := false
	var usage *llm.Usage

	for scanner.Scan() {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			events <- llm.StreamEvent{
				Type:  llm.StreamEventError,
				Error: ctx.Err(),
			}
			return
		default:
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			if !doneSent {
				events <- llm.StreamEvent{Type: llm.StreamEventDone, Usage: usage}
				doneSent = true
			}
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Error != nil {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventError,
				Error: fmt.Errorf("openrouter: %s", chunk.Error.Message),
			}
			return
		}

		// Capture usage from any chunk that includes it.
		if chunk.Usage != nil {
			usage = &llm.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
				Cost:         chunk.Usage.Cost,
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Reasoning content delta.
		if choice.Delta.ReasoningContent != "" {
			events <- llm.StreamEvent{
				Type:      llm.StreamEventReasoning,
				Reasoning: choice.Delta.ReasoningContent,
			}
		}

		// Text content delta.
		if choice.Delta.Content != "" {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDelta,
				Delta: choice.Delta.Content,
			}
		}

		// Tool call deltas.
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
			}
		}

		// Emit tool calls on finish, but keep reading for usage data.
		if choice.FinishReason != nil && (*choice.FinishReason == "tool_calls" || *choice.FinishReason == "stop") {
			emitToolCalls(activeTools, events)
		}
	}

	// If the stream ended without a finish_reason, emit whatever we have.
	if !doneSent {
		emitToolCalls(activeTools, events)
		events <- llm.StreamEvent{Type: llm.StreamEventDone, Usage: usage}
	}
}

func emitToolCalls(activeTools map[int]*toolAccum, events chan<- llm.StreamEvent) {
	for idx, accum := range activeTools {
		var args map[string]any
		if accum.argsBuf.Len() > 0 {
			_ = json.Unmarshal([]byte(accum.argsBuf.String()), &args)
		}
		events <- llm.StreamEvent{
			Type: llm.StreamEventToolCall,
			ToolCall: &llm.ToolCall{
				ID:        accum.id,
				Name:      accum.name,
				Arguments: args,
			},
		}
		delete(activeTools, idx)
	}
}
