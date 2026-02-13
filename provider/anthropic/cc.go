package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/codewandler/cc-sdk-go/oai"
	"github.com/codewandler/llm"
)

const claudeCodeProviderName = "anthropic:claude-code"

// ClaudeCodeProvider implements the provider interface using the Claude Code CLI via cc-sdk-go.
// It wraps the oai.Client which spawns the official claude CLI as a subprocess.
type ClaudeCodeProvider struct {
	client *oai.Client
}

// NewClaudeCodeProvider creates a new Claude Code provider with default settings.
// The claude CLI must be authenticated (claude setup-token) and available in PATH.
func NewClaudeCodeProvider() *ClaudeCodeProvider {
	return &ClaudeCodeProvider{
		client: oai.NewClientDefault(),
	}
}

// NewCCWithClient creates a Claude Code provider with a custom oai.Client.
// Use this when you need to customize the underlying cchat.Client configuration
// (e.g. CLI path, max concurrency, working directory).
func NewCCWithClient(client *oai.Client) *ClaudeCodeProvider {
	return &ClaudeCodeProvider{client: client}
}

func (p *ClaudeCodeProvider) Name() string {
	return claudeCodeProviderName
}

func (p *ClaudeCodeProvider) Models() []llm.Model {
	return []llm.Model{
		{ID: "sonnet", Name: "Claude Sonnet (via Claude Code)", Provider: claudeCodeProviderName},
		{ID: "opus", Name: "Claude Opus (via Claude Code)", Provider: claudeCodeProviderName},
		{ID: "haiku", Name: "Claude Haiku (via Claude Code)", Provider: claudeCodeProviderName},
	}
}

func (p *ClaudeCodeProvider) SendMessage(ctx context.Context, opts llm.SendOptions) (<-chan llm.StreamEvent, error) {
	// Convert our types to OAI types
	req := oai.ChatCompletionRequest{
		Model:  opts.Model,
		Stream: true, // Always use streaming
	}

	// Convert messages
	for _, msg := range opts.Messages {
		oaiMsg := oai.ChatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}

		// Convert tool calls if present
		if len(msg.ToolCalls) > 0 {
			oaiMsg.ToolCalls = make([]oai.ToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				oaiMsg.ToolCalls[i] = oai.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oai.FunctionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				}
			}
		}

		req.Messages = append(req.Messages, oaiMsg)
	}

	// Convert tools
	if len(opts.Tools) > 0 {
		req.Tools = make([]oai.Tool, len(opts.Tools))
		for i, t := range opts.Tools {
			req.Tools[i] = oai.Tool{
				Type: "function",
				Function: oai.FunctionDefinition{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			}
		}
	}

	// Create streaming request
	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("cc provider: %w", err)
	}

	// Create event channel and spawn goroutine to convert OAI stream to our events
	events := make(chan llm.StreamEvent, 64)
	go p.streamEvents(ctx, stream, events)

	return events, nil
}

func (p *ClaudeCodeProvider) streamEvents(ctx context.Context, stream *oai.ChatCompletionStream, events chan<- llm.StreamEvent) {
	defer close(events)
	defer stream.Close()

	var usage llm.Usage
	var toolCalls []oai.ToolCall

	for {
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

		chunk, err := stream.Recv()
		if err == io.EOF {
			// Send final done event
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDone,
				Usage: &usage,
			}
			return
		}

		if err != nil {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventError,
				Error: fmt.Errorf("stream error: %w", err),
			}
			return
		}

		// Process chunk
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]

		// Handle content delta
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDelta,
				Delta: *choice.Delta.Content,
			}
		}

		// Handle tool calls - they come already complete in the chunk
		if len(choice.Delta.ToolCalls) > 0 {
			toolCalls = append(toolCalls, choice.Delta.ToolCalls...)
		}

		// Handle finish reason
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			// If we have accumulated tool calls, emit them now
			if len(toolCalls) > 0 {
				for _, tc := range toolCalls {
					if tc.Function.Name == "" {
						continue
					}

					// Parse arguments
					var args map[string]any
					if tc.Function.Arguments != "" {
						_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
					}

					events <- llm.StreamEvent{
						Type: llm.StreamEventToolCall,
						ToolCall: &llm.ToolCall{
							ID:        tc.ID,
							Name:      tc.Function.Name,
							Arguments: args,
						},
					}
				}
				toolCalls = nil // Reset for potential next message
			}
		}

		// Handle usage (if provided in chunk)
		if chunk.Usage != nil {
			usage.InputTokens = chunk.Usage.PromptTokens
			usage.OutputTokens = chunk.Usage.CompletionTokens
			usage.TotalTokens = chunk.Usage.TotalTokens
		}
	}
}
