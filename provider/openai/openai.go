package openai

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
	baseURL      = "https://api.openai.com"
	providerName = "openai"

	// DefaultModel is the recommended default model (fast and capable)
	DefaultModel = "gpt-4o-mini"
)

// Provider implements the OpenAI LLM backend.
type Provider struct {
	apiKey       string
	defaultModel string
	client       *http.Client
}

// New creates a new OpenAI provider.
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

// Models returns a curated list of popular OpenAI models.
func (p *Provider) Models() []llm.Model {
	return []llm.Model{
		// GPT-4o series (latest, most capable)
		{ID: "gpt-4o", Name: "GPT-4o", Provider: providerName},
		{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: providerName},

		// GPT-5 series (newest generation)
		{ID: "gpt-5", Name: "GPT-5", Provider: providerName},
		{ID: "gpt-5.2", Name: "GPT-5.2", Provider: providerName},
		{ID: "gpt-5.2-pro", Name: "GPT-5.2 Pro", Provider: providerName},
		{ID: "gpt-5.1", Name: "GPT-5.1", Provider: providerName},
		{ID: "gpt-5-pro", Name: "GPT-5 Pro", Provider: providerName},
		{ID: "gpt-5-mini", Name: "GPT-5 Mini", Provider: providerName},
		{ID: "gpt-5-nano", Name: "GPT-5 Nano", Provider: providerName},
		{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Provider: providerName},
		{ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", Provider: providerName},

		// GPT-4.1 series
		{ID: "gpt-4.1", Name: "GPT-4.1", Provider: providerName},
		{ID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", Provider: providerName},
		{ID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", Provider: providerName},

		// GPT-4 series (previous generation)
		{ID: "gpt-4-turbo", Name: "GPT-4 Turbo", Provider: providerName},
		{ID: "gpt-4", Name: "GPT-4", Provider: providerName},

		// GPT-3.5 series (legacy)
		{ID: "gpt-3.5-turbo", Name: "GPT-3.5 Turbo", Provider: providerName},

		// o-series (reasoning models)
		{ID: "o3", Name: "o3", Provider: providerName},
		{ID: "o3-mini", Name: "o3 Mini", Provider: providerName},
		{ID: "o3-pro", Name: "o3 Pro", Provider: providerName},
		{ID: "o1", Name: "o1", Provider: providerName},
		{ID: "o1-pro", Name: "o1 Pro", Provider: providerName},
	}
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	body, err := buildRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go parseStream(ctx, resp.Body, events)
	return events, nil
}

// --- Request building ---

type request struct {
	Model      string           `json:"model"`
	Messages   []messagePayload `json:"messages"`
	Tools      []toolPayload    `json:"tools,omitempty"`
	ToolChoice any              `json:"tool_choice,omitempty"`
	Stream     bool             `json:"stream"`
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

func buildRequest(opts llm.StreamOptions) ([]byte, error) {
	r := request{
		Model:  opts.Model,
		Stream: true,
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

	// Set tool_choice based on opts.ToolChoice
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

	for _, msg := range opts.Messages {
		switch m := msg.(type) {
		case *llm.SystemMsg:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "system",
				Content: m.Content,
			})

		case *llm.UserMsg:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "user",
				Content: m.Content,
			})

		case *llm.AssistantMsg:
			mp := messagePayload{
				Role:    "assistant",
				Content: m.Content,
			}
			for _, tc := range m.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
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

		case *llm.ToolCallResult:
			r.Messages = append(r.Messages, messagePayload{
				Role:       "tool",
				Content:    m.Output,
				ToolCallID: m.ToolCallID,
			})
		}
	}

	return json.Marshal(r)
}

// --- SSE stream parsing ---

type streamChunk struct {
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
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
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
	var finalUsage *llm.Usage

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
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDone,
				Usage: finalUsage,
			}
			return
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		// Parse usage if present
		if chunk.Usage != nil {
			finalUsage = &llm.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Handle tool calls
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

		// Handle text content
		if choice.Delta.Content != "" {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDelta,
				Delta: choice.Delta.Content,
			}
		}

		// Handle finish reason
		if choice.FinishReason != nil {
			if *choice.FinishReason == "tool_calls" {
				emitToolCalls(activeTools, events)
			}
			// Don't emit Done here - wait for [DONE] message
		}
	}

	if err := scanner.Err(); err != nil {
		events <- llm.StreamEvent{
			Type:  llm.StreamEventError,
			Error: fmt.Errorf("stream scan error: %w", err),
		}
	}
}

func emitToolCalls(activeTools map[int]*toolAccum, events chan<- llm.StreamEvent) {
	for _, accum := range activeTools {
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
	}
}
