package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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
	region              string
	regionPrefix        string // inference profile prefix: "eu", "us", "apac", or "global"
	defaultModel        string
	credentialsProvider aws.CredentialsProvider

	mu        sync.Mutex // protects client initialization
	client    *bedrockruntime.Client
	clientErr error // deferred client creation error
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

// WithCredentialsProvider sets a custom AWS credentials provider.
// When set, the AWS client is created lazily on first use, allowing
// credentials to be fetched at request time rather than at construction.
// This enables integration with external secret managers or dynamic
// credential sources.
func WithCredentialsProvider(cp aws.CredentialsProvider) Option {
	return func(p *Provider) {
		p.credentialsProvider = cp
	}
}

// New creates a new AWS Bedrock provider.
// The provider uses the AWS SDK's default credential chain:
//   - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//   - Shared credentials file (~/.aws/credentials)
//   - IAM role (for EC2/ECS/Lambda)
//   - SSO credentials
//
// When WithCredentialsProvider is used, client creation is deferred until
// the first CreateStream call, allowing credentials to be fetched lazily.
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
//
//	// Use custom credentials provider (lazy initialization)
//	p := bedrock.New(bedrock.WithCredentialsProvider(myProvider))
func New(opts ...Option) *Provider {
	p := &Provider{
		region:       "us-east-1",
		defaultModel: DefaultModel,
	}

	for _, opt := range opts {
		opt(p)
	}

	// Compute region prefix for inference profiles
	p.regionPrefix = computeRegionPrefix(p.region)

	// If custom credentials provider is set, defer client creation
	// to first use (lazy initialization)
	if p.credentialsProvider != nil {
		return p
	}

	// Create AWS SDK client immediately using default credential chain
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

// initClient creates the AWS client lazily if not already initialized.
// Thread-safe: uses mutex to ensure only one goroutine creates the client.
func (p *Provider) initClient(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Already initialized (success or failure)?
	if p.client != nil || p.clientErr != nil {
		return p.clientErr
	}

	// Build config options
	configOpts := []func(*config.LoadOptions) error{
		config.WithRegion(p.region),
	}
	if p.credentialsProvider != nil {
		configOpts = append(configOpts, config.WithCredentialsProvider(p.credentialsProvider))
	}

	cfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		p.clientErr = fmt.Errorf("load AWS config: %w", err)
		return p.clientErr
	}

	p.client = bedrockruntime.NewFromConfig(cfg)
	return nil
}

// resolveModel resolves a model ID to include the appropriate inference profile prefix.
//
// Resolution order:
//  1. If model already has a region prefix (eu., us., etc.) - passthrough
//  2. If model is in inference profile registry - apply regional prefix
//  3. If regional prefix not available - fall back to global
//  4. If no profile exists - return model unchanged
//
// Returns the resolved model ID and any error.
func (p *Provider) resolveModel(model string) (string, error) {
	// 1. Already has region prefix - passthrough
	if hasRegionPrefix(model) {
		return model, nil
	}

	// 2. Check if model has inference profile
	profile, ok := inferenceProfiles[model]
	if !ok {
		// No profile - use model as-is (may work for some models)
		return model, nil
	}

	// 3. Try region-specific prefix first
	if containsPrefix(profile.Prefixes, p.regionPrefix) {
		return p.regionPrefix + "." + model, nil
	}

	// 4. Fallback to global
	if containsPrefix(profile.Prefixes, "global") {
		return "global." + model, nil
	}

	// 5. No valid prefix available - return error
	return "", fmt.Errorf("model %q not available in region %s (available: %v)",
		model, p.region, profile.Prefixes)
}

// RegionPrefix returns the computed inference profile prefix for this provider's region.
// Returns "eu", "us", "apac", or "global".
func (p *Provider) RegionPrefix() string {
	return p.regionPrefix
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	// Lazy client initialization (thread-safe)
	if err := p.initClient(ctx); err != nil {
		return nil, err
	}

	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	// Resolve model to include inference profile prefix
	resolvedModel, err := p.resolveModel(opts.Model)
	if err != nil {
		return nil, err
	}

	// Create a copy of opts with resolved model
	resolvedOpts := opts
	resolvedOpts.Model = resolvedModel

	input, err := buildRequest(resolvedOpts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	startTime := time.Now()
	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock request: %w", err)
	}

	meta := streamMeta{
		RequestedModel: opts.Model,    // Original user-provided model
		ResolvedModel:  resolvedModel, // Resolved with inference profile prefix
		StartTime:      startTime,
	}
	events := make(chan llm.StreamEvent, 64)
	go parseStream(ctx, output, events, meta)
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

// streamMeta passes context into the stream parser for StreamEventStart.
type streamMeta struct {
	RequestedModel string
	ResolvedModel  string
	StartTime      time.Time
}

func parseStream(ctx context.Context, output *bedrockruntime.ConverseStreamOutput, events chan<- llm.StreamEvent, meta streamMeta) {
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
	startEmitted := false

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

		// Emit StreamEventStart on first event
		if !startEmitted {
			startEmitted = true
			events <- llm.StreamEvent{
				Type: llm.StreamEventStart,
				Start: &llm.StreamStart{
					RequestedModel:   meta.RequestedModel,
					ResolvedModel:    meta.ResolvedModel,
					ProviderModel:    "", // Bedrock doesn't return model in stream
					RequestID:        "", // Bedrock doesn't return request ID in stream
					TimeToFirstToken: time.Since(meta.StartTime),
				},
			}
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
				usage.Cost = calculateCost(meta.ResolvedModel, &usage)
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
