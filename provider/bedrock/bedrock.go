package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/codewandler/llm"
)

const (
	providerName = "bedrock"

	// DefaultModel is the recommended default model (Claude Sonnet 4).
	DefaultModel = "anthropic.claude-sonnet-4-20250514-v1:0"
)

// Provider implements the AWS Bedrock LLM backend.
type Provider struct {
	region       string
	defaultModel string
	client       *bedrockruntime.Client
	clientErr    error // deferred client creation error
}

// Option configures a Bedrock provider.
type Option func(*Provider)

// WithRegion sets the AWS region for Bedrock.
// Defaults to us-east-1 if not specified.
func WithRegion(region string) Option {
	return func(p *Provider) {
		p.region = region
	}
}

// WithDefaultModel sets the default model ID.
func WithDefaultModel(model string) Option {
	return func(p *Provider) {
		p.defaultModel = model
	}
}

// New creates a new AWS Bedrock provider.
// The provider uses the AWS SDK's default credential chain:
//   - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//   - Shared credentials file (~/.aws/credentials)
//   - IAM role (for EC2/ECS/Lambda)
//   - SSO credentials
//
// Example usage:
//
//	// Use defaults (us-east-1, default credentials)
//	p := bedrock.New()
//
//	// Specify region
//	p := bedrock.New(bedrock.WithRegion("us-west-2"))
//
//	// Specify default model
//	p := bedrock.New(bedrock.WithDefaultModel("anthropic.claude-3-5-haiku-20241022-v1:0"))
func New(opts ...Option) *Provider {
	p := &Provider{
		region:       "us-east-1",
		defaultModel: DefaultModel,
	}

	for _, opt := range opts {
		opt(p)
	}

	// Create AWS SDK client
	// We defer errors to CreateStream so New() never fails
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(p.region),
	)
	if err != nil {
		p.clientErr = fmt.Errorf("load AWS config: %w", err)
		return p
	}

	p.client = bedrockruntime.NewFromConfig(cfg)
	return p
}

func (p *Provider) Name() string { return providerName }

// DefaultModel returns the configured default model ID.
func (p *Provider) DefaultModel() string {
	return p.defaultModel
}

// Models returns a curated list of popular Bedrock models.
func (p *Provider) Models() []llm.Model {
	models := make([]llm.Model, 0, len(modelOrder))
	for _, id := range modelOrder {
		if info, ok := modelRegistry[id]; ok {
			models = append(models, llm.Model{
				ID:       info.ID,
				Name:     info.Name,
				Provider: providerName,
			})
		}
	}
	return models
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	// Check for deferred client creation error
	if p.clientErr != nil {
		return nil, p.clientErr
	}

	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	input, err := buildRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock request: %w", err)
	}

	events := make(chan llm.StreamEvent, 64)
	go parseStream(ctx, output, events, opts.Model)
	return events, nil
}

// --- Request building ---

func buildRequest(opts llm.StreamOptions) (*bedrockruntime.ConverseStreamInput, error) {
	input := &bedrockruntime.ConverseStreamInput{
		ModelId: aws.String(opts.Model),
	}

	// Convert messages
	var system []types.SystemContentBlock
	var messages []types.Message

	for i := 0; i < len(opts.Messages); i++ {
		msg := opts.Messages[i]

		switch m := msg.(type) {
		case *llm.SystemMsg:
			system = append(system, &types.SystemContentBlockMemberText{
				Value: m.Content,
			})

		case *llm.UserMsg:
			messages = append(messages, types.Message{
				Role: types.ConversationRoleUser,
				Content: []types.ContentBlock{
					&types.ContentBlockMemberText{Value: m.Content},
				},
			})

		case *llm.AssistantMsg:
			var content []types.ContentBlock
			if m.Content != "" {
				content = append(content, &types.ContentBlockMemberText{Value: m.Content})
			}
			for _, tc := range m.ToolCalls {
				// Convert arguments to document.Interface
				inputDoc, err := toDocument(tc.Arguments)
				if err != nil {
					return nil, fmt.Errorf("marshal tool arguments: %w", err)
				}
				content = append(content, &types.ContentBlockMemberToolUse{
					Value: types.ToolUseBlock{
						ToolUseId: aws.String(tc.ID),
						Name:      aws.String(tc.Name),
						Input:     inputDoc,
					},
				})
			}
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleAssistant,
				Content: content,
			})

		case *llm.ToolCallResult:
			// Bedrock expects tool results in a user message with toolResult content blocks
			// Collect consecutive tool results
			var toolResults []types.ContentBlock
			for ; i < len(opts.Messages); i++ {
				tr, ok := opts.Messages[i].(*llm.ToolCallResult)
				if !ok {
					break
				}
				status := types.ToolResultStatusSuccess
				if tr.IsError {
					status = types.ToolResultStatusError
				}
				toolResults = append(toolResults, &types.ContentBlockMemberToolResult{
					Value: types.ToolResultBlock{
						ToolUseId: aws.String(tr.ToolCallID),
						Content: []types.ToolResultContentBlock{
							&types.ToolResultContentBlockMemberText{Value: tr.Output},
						},
						Status: status,
					},
				})
			}
			i-- // back up one since loop will increment
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleUser,
				Content: toolResults,
			})
		}
	}

	if len(system) > 0 {
		input.System = system
	}
	input.Messages = messages

	// Convert tools
	if len(opts.Tools) > 0 {
		var tools []types.Tool
		for _, t := range opts.Tools {
			schema, err := toDocument(t.Parameters)
			if err != nil {
				return nil, fmt.Errorf("marshal tool schema: %w", err)
			}
			tools = append(tools, &types.ToolMemberToolSpec{
				Value: types.ToolSpecification{
					Name:        aws.String(t.Name),
					Description: aws.String(t.Description),
					InputSchema: &types.ToolInputSchemaMemberJson{
						Value: schema,
					},
				},
			})
		}

		toolConfig := &types.ToolConfiguration{
			Tools: tools,
		}

		// Set tool choice
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			toolConfig.ToolChoice = &types.ToolChoiceMemberAuto{Value: types.AutoToolChoice{}}
		case llm.ToolChoiceRequired:
			toolConfig.ToolChoice = &types.ToolChoiceMemberAny{Value: types.AnyToolChoice{}}
		case llm.ToolChoiceNone:
			// Don't set tools at all for "none"
			toolConfig = nil
		case llm.ToolChoiceTool:
			toolConfig.ToolChoice = &types.ToolChoiceMemberTool{
				Value: types.SpecificToolChoice{
					Name: aws.String(tc.Name),
				},
			}
		}

		if toolConfig != nil {
			input.ToolConfig = toolConfig
		}
	}

	return input, nil
}

// toDocument converts a Go value to a Smithy document.Interface.
func toDocument(v any) (document.Interface, error) {
	// Marshal to JSON and unmarshal to interface{} to normalize the type
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(jsonBytes, &normalized); err != nil {
		return nil, err
	}
	return document.NewLazyDocument(normalized), nil
}

// --- Stream parsing ---

func parseStream(ctx context.Context, output *bedrockruntime.ConverseStreamOutput, events chan<- llm.StreamEvent, model string) {
	defer close(events)

	stream := output.GetStream()
	defer stream.Close()

	// Tool call accumulation
	type toolAccum struct {
		id      string
		name    string
		argsBuf strings.Builder
	}
	activeTools := make(map[int]*toolAccum)
	var usage llm.Usage

	for event := range stream.Events() {
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

		switch e := event.(type) {
		case *types.ConverseStreamOutputMemberContentBlockStart:
			// Start of a new content block (text or tool_use)
			idx := int(aws.ToInt32(e.Value.ContentBlockIndex))
			if e.Value.Start != nil {
				switch start := e.Value.Start.(type) {
				case *types.ContentBlockStartMemberToolUse:
					activeTools[idx] = &toolAccum{
						id:   aws.ToString(start.Value.ToolUseId),
						name: aws.ToString(start.Value.Name),
					}
				}
			}

		case *types.ConverseStreamOutputMemberContentBlockDelta:
			// Delta within a content block
			idx := int(aws.ToInt32(e.Value.ContentBlockIndex))
			if e.Value.Delta != nil {
				switch delta := e.Value.Delta.(type) {
				case *types.ContentBlockDeltaMemberText:
					events <- llm.StreamEvent{
						Type:  llm.StreamEventDelta,
						Delta: delta.Value,
					}
				case *types.ContentBlockDeltaMemberToolUse:
					// Accumulate tool input JSON
					if tb, ok := activeTools[idx]; ok && delta.Value.Input != nil {
						tb.argsBuf.WriteString(*delta.Value.Input)
					}
				}
			}

		case *types.ConverseStreamOutputMemberContentBlockStop:
			// End of a content block - emit tool call if it was a tool
			idx := int(aws.ToInt32(e.Value.ContentBlockIndex))
			if tb, ok := activeTools[idx]; ok {
				var args map[string]any
				if tb.argsBuf.Len() > 0 {
					_ = json.Unmarshal([]byte(tb.argsBuf.String()), &args)
				}
				events <- llm.StreamEvent{
					Type: llm.StreamEventToolCall,
					ToolCall: &llm.ToolCall{
						ID:        tb.id,
						Name:      tb.name,
						Arguments: args,
					},
				}
				delete(activeTools, idx)
			}

		case *types.ConverseStreamOutputMemberMetadata:
			// Usage information - this comes after MessageStop
			if e.Value.Usage != nil {
				if e.Value.Usage.InputTokens != nil {
					usage.InputTokens = int(*e.Value.Usage.InputTokens)
				}
				if e.Value.Usage.OutputTokens != nil {
					usage.OutputTokens = int(*e.Value.Usage.OutputTokens)
				}
				if e.Value.Usage.TotalTokens != nil {
					usage.TotalTokens = int(*e.Value.Usage.TotalTokens)
				}
				usage.Cost = calculateCost(model, &usage)
			}
			// Emit done event with usage after metadata is received
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDone,
				Usage: &usage,
			}
			return

		case *types.ConverseStreamOutputMemberMessageStop:
			// Message complete - but continue to receive metadata event
			// Don't return here, the metadata event comes after
		}
	}

	// Check for stream errors
	if err := stream.Err(); err != nil {
		events <- llm.StreamEvent{
			Type:  llm.StreamEventError,
			Error: fmt.Errorf("bedrock stream: %w", err),
		}
	}
}
