package anthropic

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
	providerName     = "anthropic"
	defaultBaseURL   = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"
)

// Provider implements the direct Anthropic API backend.
type Provider struct {
	opts   *llm.Options
	client *http.Client
}

// DefaultOptions returns the default options for Anthropic.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
		llm.APIKeyFromEnv("ANTHROPIC_API_KEY"),
	}
}

// New creates a new Anthropic provider.
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	return &Provider{opts: cfg, client: &http.Client{}}
}

func (p *Provider) Name() string { return providerName }

func (p *Provider) Models() []llm.Model {
	return []llm.Model{
		{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", Provider: providerName},
		{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Provider: providerName},
	}
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	apiKey, err := p.opts.ResolveAPIKey(ctx)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic API key is not configured")
	}

	body, err := buildAnthropicRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := p.newAPIRequest(ctx, apiKey, body)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go parseStream(ctx, resp.Body, events)
	return events, nil
}

func (p *Provider) newAPIRequest(ctx context.Context, apiKey string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("x-api-key", apiKey)
	return req, nil
}

type request struct {
	Model      string           `json:"model"`
	MaxTokens  int              `json:"max_tokens"`
	Stream     bool             `json:"stream"`
	System     any              `json:"system,omitempty"`
	Messages   []messagePayload `json:"messages"`
	Tools      []toolPayload    `json:"tools,omitempty"`
	ToolChoice any              `json:"tool_choice,omitempty"`
	Metadata   *metadata        `json:"metadata,omitempty"`
}

type metadata struct {
	UserID string `json:"user_id"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type systemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type toolPayload struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

func buildAnthropicRequest(opts llm.StreamOptions) ([]byte, error) {
	r := request{Model: opts.Model, MaxTokens: 16384, Stream: true}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	if len(opts.Tools) > 0 {
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = map[string]string{"type": "auto"}
		case llm.ToolChoiceRequired:
			r.ToolChoice = map[string]string{"type": "any"}
		case llm.ToolChoiceNone:
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "tool", "name": tc.Name}
		}
	}

	for i := 0; i < len(opts.Messages); i++ {
		switch m := opts.Messages[i].(type) {
		case *llm.SystemMsg:
			r.System = m.Content
		case *llm.UserMsg:
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: m.Content})
		case *llm.AssistantMsg:
			if len(m.ToolCalls) == 0 {
				r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: m.Content})
				continue
			}
			var blocks []contentBlock
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, contentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: tc.Arguments})
			}
			r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: blocks})
		case *llm.ToolCallResult:
			var results []contentBlock
			prevAssistant := findPrecedingAssistant(opts.Messages, i)
			toolIdx := 0
			for ; i < len(opts.Messages); i++ {
				tr, ok := opts.Messages[i].(*llm.ToolCallResult)
				if !ok {
					break
				}
				toolUseID := tr.ToolCallID
				if toolUseID == "" && prevAssistant != nil && toolIdx < len(prevAssistant.ToolCalls) {
					toolUseID = prevAssistant.ToolCalls[toolIdx].ID
				}
				results = append(results, contentBlock{Type: "tool_result", ToolUseID: toolUseID, Content: tr.Output, IsError: tr.IsError})
				toolIdx++
			}
			i--
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: results})
		}
	}

	return json.Marshal(r)
}

func findPrecedingAssistant(messages llm.Messages, toolIdx int) *llm.AssistantMsg {
	for j := toolIdx - 1; j >= 0; j-- {
		if am, ok := messages[j].(*llm.AssistantMsg); ok {
			return am
		}
	}
	return nil
}

type contentBlockStartEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"content_block"`
}

type contentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

func parseStream(ctx context.Context, body io.ReadCloser, events chan<- llm.StreamEvent) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	type toolBlock struct {
		id      string
		name    string
		jsonBuf strings.Builder
	}
	activeTools := make(map[int]*toolBlock)
	var usage llm.Usage

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			events <- llm.StreamEvent{Type: llm.StreamEventError, Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &base); err != nil {
			continue
		}

		switch base.Type {
		case "message_start":
			var evt struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				usage.InputTokens = evt.Message.Usage.InputTokens
			}

		case "message_delta":
			var evt struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				usage.OutputTokens = evt.Usage.OutputTokens
				usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			}

		case "content_block_start":
			var evt contentBlockStartEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			if evt.ContentBlock.Type == "tool_use" {
				activeTools[evt.Index] = &toolBlock{id: evt.ContentBlock.ID, name: evt.ContentBlock.Name}
			}

		case "content_block_delta":
			var evt contentBlockDeltaEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			switch evt.Delta.Type {
			case "text_delta":
				events <- llm.StreamEvent{Type: llm.StreamEventDelta, Delta: evt.Delta.Text}
			case "input_json_delta":
				if tb, ok := activeTools[evt.Index]; ok {
					tb.jsonBuf.WriteString(evt.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			var evt struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			if tb, ok := activeTools[evt.Index]; ok {
				var args map[string]any
				if tb.jsonBuf.Len() > 0 {
					_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
				}
				events <- llm.StreamEvent{Type: llm.StreamEventToolCall, ToolCall: &llm.ToolCall{ID: tb.id, Name: tb.name, Arguments: args}}
				delete(activeTools, evt.Index)
			}

		case "message_stop":
			events <- llm.StreamEvent{Type: llm.StreamEventDone, Usage: &usage}
			return

		case "error":
			var errEvt struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errEvt); err == nil {
				events <- llm.StreamEvent{Type: llm.StreamEventError, Error: fmt.Errorf("anthropic: %s", errEvt.Error.Message)}
			}
			return
		}
	}
}
