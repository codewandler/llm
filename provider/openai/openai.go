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
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}
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

// --- Model classification for reasoning effort ---

// modelCategory identifies reasoning support level for a model.
type modelCategory int

const (
	categoryNonReasoning modelCategory = iota // gpt-4o, gpt-4, gpt-3.5, gpt-4.1
	categoryPreGPT51                          // gpt-5, gpt-5-mini, gpt-5-nano, o1, o3
	categoryGPT51                             // gpt-5.1
	categoryGPT5Pro                           // gpt-5-pro, gpt-5.2-pro, o3-pro, o1-pro
	categoryCodexMax                          // gpt-5.1-codex-max and later codex models
)

// classifyModel determines the reasoning category for a model.
func classifyModel(model string) modelCategory {
	m := strings.ToLower(model)

	// Pro models: only support "high"
	if strings.HasSuffix(m, "-pro") {
		return categoryGPT5Pro
	}

	// Codex-max models: support xhigh
	if strings.Contains(m, "codex-max") || strings.Contains(m, "codex") && strings.Contains(m, "5.1") {
		return categoryCodexMax
	}
	if strings.Contains(m, "codex") && strings.Contains(m, "5.2") {
		return categoryCodexMax
	}

	// gpt-5.1 (not codex, not pro): supports none, low, medium, high (NOT minimal)
	if strings.HasPrefix(m, "gpt-5.1") {
		return categoryGPT51
	}

	// Pre-5.1 reasoning models: gpt-5, gpt-5-mini, gpt-5-nano, gpt-5.2, o1, o3
	if strings.HasPrefix(m, "gpt-5") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") {
		return categoryPreGPT51
	}

	// Everything else: non-reasoning (gpt-4o, gpt-4, gpt-3.5, gpt-4.1, etc.)
	return categoryNonReasoning
}

// mapReasoningEffort maps the user-requested reasoning effort to a valid OpenAI API value.
// Returns empty string if the parameter should be omitted, or an error if the value is invalid.
func mapReasoningEffort(model string, effort llm.ReasoningEffort) (string, error) {
	if effort == "" {
		return "", nil // omit, let API use its default
	}

	cat := classifyModel(model)

	switch cat {
	case categoryNonReasoning:
		// Non-reasoning models ignore reasoning_effort
		return "", nil

	case categoryPreGPT51:
		// Supports: minimal, low, medium, high
		// Does NOT support: none, xhigh
		switch effort {
		case llm.ReasoningEffortNone:
			return "", fmt.Errorf("reasoning_effort %q not supported for model %q (use minimal, low, medium, or high)", effort, model)
		case llm.ReasoningEffortXHigh:
			return "", fmt.Errorf("reasoning_effort %q not supported for model %q (use minimal, low, medium, or high)", effort, model)
		case llm.ReasoningEffortMinimal, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh:
			return string(effort), nil
		}

	case categoryGPT51:
		// Supports: none, low, medium, high
		// Does NOT support: minimal, xhigh
		// Map minimal -> low
		switch effort {
		case llm.ReasoningEffortMinimal:
			return "low", nil // map minimal -> low
		case llm.ReasoningEffortXHigh:
			return "", fmt.Errorf("reasoning_effort %q not supported for model %q (use none, low, medium, or high)", effort, model)
		case llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh:
			return string(effort), nil
		}

	case categoryGPT5Pro:
		// Only supports: high
		if effort != llm.ReasoningEffortHigh {
			return "", fmt.Errorf("reasoning_effort must be %q for model %q", llm.ReasoningEffortHigh, model)
		}
		return "high", nil

	case categoryCodexMax:
		// Supports: none, low, medium, high, xhigh
		// Map minimal -> low
		switch effort {
		case llm.ReasoningEffortMinimal:
			return "low", nil // map minimal -> low
		case llm.ReasoningEffortNone, llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh:
			return string(effort), nil
		}
	}

	// Unknown effort value - shouldn't happen if Valid() was called
	return "", fmt.Errorf("unknown reasoning_effort value %q", effort)
}

// --- Request building ---

type request struct {
	Model           string           `json:"model"`
	Messages        []messagePayload `json:"messages"`
	Tools           []toolPayload    `json:"tools,omitempty"`
	ToolChoice      any              `json:"tool_choice,omitempty"`
	ReasoningEffort string           `json:"reasoning_effort,omitempty"`
	Stream          bool             `json:"stream"`
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

	// Set reasoning_effort based on model category
	reasoningEffort, err := mapReasoningEffort(opts.Model, opts.ReasoningEffort)
	if err != nil {
		return nil, err
	}
	if reasoningEffort != "" {
		r.ReasoningEffort = reasoningEffort
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
