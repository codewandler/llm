package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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

const providerName = "bedrock"

// DefaultModel is the recommended default model (Claude Sonnet 4.6).
var DefaultModel = ModelSonnetLatest

// Provider implements the AWS Bedrock LLM backend.
type Provider struct {
	region              string
	regionPrefix        string // inference profile prefix: PrefixEU, PrefixUS, PrefixAPAC, or PrefixGlobal
	defaultModel        string
	profile             string // AWS profile name (optional)
	credentialsProvider aws.CredentialsProvider
	httpClient          *http.Client // HTTP client passed to the AWS SDK
	logger              *slog.Logger // optional stream event logger

	mu        sync.Mutex // protects client initialization
	client    *bedrockruntime.Client
	clientErr error // deferred client creation error
}

// Option configures a Bedrock provider.
type Option func(*Provider)

// WithLLMOptions applies one or more llm.Option values to the Bedrock provider.
// This allows using shared llm options (e.g. llm.WithHTTPClient) with this provider.
//
// Example:
//
//	bedrock.New(bedrock.WithLLMOptions(llm.WithHTTPClient(myClient)))
func WithLLMOptions(opts ...llm.Option) Option {
	return func(p *Provider) {
		cfg := llm.Apply(opts...)
		if cfg.HTTPClient != nil {
			p.httpClient = cfg.HTTPClient
		}
		if cfg.Logger != nil {
			p.logger = cfg.Logger
		}
	}
}

// WithRegion sets the AWS region for Bedrock explicitly.
// By default, New() reads the region from AWS_REGION or AWS_DEFAULT_REGION
// environment variables, falling back to DefaultRegion (us-east-1).
func WithRegion(region string) Option {
	return func(p *Provider) {
		p.region = region
	}
}

// WithRegionFromEnv reads the region from AWS_REGION or AWS_DEFAULT_REGION
// environment variables. This is useful to re-enable environment variable
// lookup after WithRegion() has been called.
func WithRegionFromEnv() Option {
	return func(p *Provider) {
		p.region = getRegionFromEnv()
	}
}

// WithProfile sets the AWS profile to use for credentials and configuration.
// This allows using a specific profile from ~/.aws/credentials or ~/.aws/config
// instead of the default profile or AWS_PROFILE environment variable.
func WithProfile(profile string) Option {
	return func(p *Provider) {
		p.profile = profile
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

// getRegionFromEnv reads the region from AWS_REGION or AWS_DEFAULT_REGION
// environment variables, falling back to DefaultRegion if neither is set.
func getRegionFromEnv() string {
	if r := os.Getenv(EnvAWSRegion); r != "" {
		return r
	}
	if r := os.Getenv(EnvAWSDefaultRegion); r != "" {
		return r
	}
	return DefaultRegion
}

// New creates a new AWS Bedrock provider.
// The provider uses the AWS SDK's default credential chain:
//   - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//   - Shared credentials file (~/.aws/credentials)
//   - IAM role (for EC2/ECS/Lambda)
//   - SSO credentials
//
// Region is determined from:
//   - AWS_REGION environment variable
//   - AWS_DEFAULT_REGION environment variable
//   - DefaultRegion (us-east-1) if neither is set
//
// The AWS_PROFILE environment variable is honored automatically by the SDK.
// Use WithProfile() to explicitly select a different profile.
//
// When WithCredentialsProvider is used, client creation is deferred until
// the first CreateStream call, allowing credentials to be fetched lazily.
//
// Example usage:
//
//	// Use defaults (reads AWS_REGION, AWS_PROFILE from env)
//	p := bedrock.New()
//
//	// Specify region explicitly
//	p := bedrock.New(bedrock.WithRegion(bedrock.RegionEUCentral1))
//
//	// Use a specific AWS profile
//	p := bedrock.New(bedrock.WithProfile("production"))
//
//	// Combine options
//	p := bedrock.New(
//	    bedrock.WithProfile("production"),
//	    bedrock.WithRegion(bedrock.RegionEUWest1),
//	)
//
//	// Specify default model
//	p := bedrock.New(bedrock.WithDefaultModel(bedrock.ModelHaikuLatest))
//
//	// Use custom credentials provider (lazy initialization)
//	p := bedrock.New(bedrock.WithCredentialsProvider(myProvider))
func New(opts ...Option) *Provider {
	p := &Provider{
		region:       getRegionFromEnv(),
		defaultModel: DefaultModel,
		httpClient:   llm.DefaultHttpClient(),
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
	configOpts := []func(*config.LoadOptions) error{
		config.WithRegion(p.region),
		config.WithHTTPClient(p.httpClient),
	}
	if p.profile != "" {
		configOpts = append(configOpts, config.WithSharedConfigProfile(p.profile))
	}
	cfg, err := config.LoadDefaultConfig(context.Background(), configOpts...)
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
	return models()
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
		config.WithHTTPClient(p.httpClient),
	}
	if p.profile != "" {
		configOpts = append(configOpts, config.WithSharedConfigProfile(p.profile))
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
	if containsPrefix(profile.Prefixes, PrefixGlobal) {
		return PrefixGlobal + "." + model, nil
	}

	// 5. No valid prefix available - return error
	return "", fmt.Errorf("model %q not available in region %s (available: %v)",
		model, p.region, profile.Prefixes)
}

// RegionPrefix returns the computed inference profile prefix for this provider's region.
// Returns PrefixEU, PrefixUS, PrefixAPAC, or PrefixGlobal.
func (p *Provider) RegionPrefix() string {
	return p.regionPrefix
}

func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamRequest) (<-chan llm.StreamEvent, error) {
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
		return nil, llm.NewErrBuildRequest(llm.ProviderNameBedrock, err)
	}

	startTime := time.Now()
	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameBedrock, err)
	}

	meta := streamMeta{
		RequestedModel: opts.Model,
		ResolvedModel:  resolvedModel,
		StartTime:      startTime,
		Logger:         p.logger,
	}
	stream := llm.NewEventStream()
	go parseStream(ctx, output, stream, meta)
	return stream.C(), nil
}

// --- Request building ---

// isClaudeModel returns true if the model ID refers to an Anthropic Claude model
// on Bedrock. Only Claude models support cachePoint blocks.
func isClaudeModel(modelID string) bool {
	return strings.HasPrefix(modelID, "anthropic.claude") ||
		strings.Contains(modelID, "anthropic.claude")
}

// buildBedrockCachePoint creates a CachePointBlock from a CacheHint.
// Returns nil if the hint is nil, not enabled, or the model doesn't support caching.
func buildBedrockCachePoint(h *llm.CacheHint, modelID string) *types.CachePointBlock {
	if h == nil || !h.Enabled || !isClaudeModel(modelID) {
		return nil
	}
	cp := &types.CachePointBlock{Type: types.CachePointTypeDefault}
	if h.TTL == "1h" {
		cp.Ttl = types.CacheTTLOneHour
	}
	return cp
}

// hasBedrockPerMessageCacheHints returns true if any message carries an enabled CacheHint.
func hasBedrockPerMessageCacheHints(msgs llm.Messages) bool {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *llm.SystemMsg:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		case *llm.UserMsg:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		case *llm.AssistantMsg:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		case *llm.ToolCallResult:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		}
	}
	return false
}

func buildRequest(opts llm.StreamRequest) (*bedrockruntime.ConverseStreamInput, error) {
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
			// Append cachePoint after this system block if requested
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				system = append(system, &types.SystemContentBlockMemberCachePoint{
					Value: *cp,
				})
			}

		case *llm.UserMsg:
			content := []types.ContentBlock{
				&types.ContentBlockMemberText{Value: m.Content},
			}
			// Append cachePoint after content if requested
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				content = append(content, &types.ContentBlockMemberCachePoint{Value: *cp})
			}
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleUser,
				Content: content,
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
			// Append cachePoint after all content blocks if requested
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				content = append(content, &types.ContentBlockMemberCachePoint{Value: *cp})
			}
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleAssistant,
				Content: content,
			})

		case *llm.ToolCallResult:
			// Bedrock expects tool results in a user message with toolResult content blocks
			// Collect consecutive tool results
			var toolResults []types.ContentBlock
			startI := i
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
			// Apply cache hint from the last ToolCallResult in this batch
			if i > startI {
				if lastTR, ok := opts.Messages[i-1].(*llm.ToolCallResult); ok {
					if cp := buildBedrockCachePoint(lastTR.CacheHint, opts.Model); cp != nil {
						toolResults = append(toolResults, &types.ContentBlockMemberCachePoint{Value: *cp})
					}
				}
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

	// Apply top-level automatic cache hint: append a cachePoint to the last message
	// when no per-message hints are present. Only applies to Claude models.
	if opts.CacheHint != nil && opts.CacheHint.Enabled &&
		!hasBedrockPerMessageCacheHints(opts.Messages) &&
		isClaudeModel(opts.Model) &&
		len(input.Messages) > 0 {
		cp := buildBedrockCachePoint(opts.CacheHint, opts.Model)
		if cp != nil {
			last := &input.Messages[len(input.Messages)-1]
			last.Content = append(last.Content, &types.ContentBlockMemberCachePoint{Value: *cp})
		}
	}

	// Convert tools
	if len(opts.Tools) > 0 {
		var tools []types.Tool
		for _, t := range opts.Tools {
			schema, err := toDocument(llm.NewSortedMap(t.Parameters))
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
	Logger         *slog.Logger
}

func parseStream(ctx context.Context, output *bedrockruntime.ConverseStreamOutput, events *llm.EventStream, meta streamMeta) {
	defer events.Close()

	stream := output.GetStream()
	defer stream.Close()

	// logEvent emits a decoded eventstream frame to the logger in the same
	// format the HTTP transport uses for SSE chunks, so httpLogHandler renders
	// it identically. v must be JSON-serialisable.
	logEvent := func(eventType string, v any) {
		if meta.Logger == nil {
			return
		}
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		// Inject the event type into the JSON so httpLogHandler can identify
		// and render it (noisy events collapsed, full events expanded).
		// Merge {"type":"<eventType>"} with the struct JSON.
		merged := `{"type":"` + eventType + `"`
		if string(b) != "{}" {
			merged += "," + string(b[1:]) // drop leading '{' from b
		} else {
			merged += "}"
		}
		meta.Logger.Debug("http response body",
			"method", "POST",
			"url", "bedrock/converse-stream",
			"chunk", "data: "+merged+"\n\n",
		)
	}

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
			events.Error(llm.NewErrContextCancelled(llm.ProviderNameBedrock, ctx.Err()))
			return
		default:
		}

		// Emit StreamEventStart on first event
		if !startEmitted {
			startEmitted = true
			events.Send(llm.StreamEvent{
				Type: llm.StreamEventStart,
				Start: &llm.StreamStart{
					ModelRequested:    meta.RequestedModel,
					ModelResolved:     meta.ResolvedModel,
					ModelProviderID:   "", // Bedrock doesn't return model in stream
					ProviderRequestID: "", // Bedrock doesn't return request ID in stream
					TimeToFirstToken:  time.Since(meta.StartTime),
				},
			})
		}

		switch e := event.(type) {
		case *types.ConverseStreamOutputMemberContentBlockStart:
			idx := int(aws.ToInt32(e.Value.ContentBlockIndex))
			logEvent("content_block_start", e.Value)
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
			idx := int(aws.ToInt32(e.Value.ContentBlockIndex))
			logEvent("content_block_delta", e.Value)
			if e.Value.Delta != nil {
				switch delta := e.Value.Delta.(type) {
				case *types.ContentBlockDeltaMemberText:
					events.Send(llm.StreamEvent{
						Type:  llm.StreamEventDelta,
						Delta: delta.Value,
					})
				case *types.ContentBlockDeltaMemberToolUse:
					if tb, ok := activeTools[idx]; ok && delta.Value.Input != nil {
						tb.argsBuf.WriteString(*delta.Value.Input)
					}
				}
			}

		case *types.ConverseStreamOutputMemberContentBlockStop:
			idx := int(aws.ToInt32(e.Value.ContentBlockIndex))
			logEvent("content_block_stop", e.Value)
			if tb, ok := activeTools[idx]; ok {
				var args map[string]any
				if tb.argsBuf.Len() > 0 {
					_ = json.Unmarshal([]byte(tb.argsBuf.String()), &args)
				}
				events.Send(llm.StreamEvent{
					Type: llm.StreamEventToolCall,
					ToolCall: &llm.ToolCall{
						ID:        tb.id,
						Name:      tb.name,
						Arguments: args,
					},
				})
				delete(activeTools, idx)
			}

		case *types.ConverseStreamOutputMemberMetadata:
			logEvent("metadata", e.Value)
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
				if e.Value.Usage.CacheReadInputTokens != nil {
					usage.CacheReadTokens = int(*e.Value.Usage.CacheReadInputTokens)
				}
				if e.Value.Usage.CacheWriteInputTokens != nil {
					usage.CacheWriteTokens = int(*e.Value.Usage.CacheWriteInputTokens)
				}
				fillCost(meta.ResolvedModel, &usage)
			}
			events.Send(llm.StreamEvent{
				Type:  llm.StreamEventDone,
				Usage: &usage,
			})
			return

		case *types.ConverseStreamOutputMemberMessageStop:
			logEvent("message_stop", e.Value)
		}
	}

	// Check for stream errors
	if err := stream.Err(); err != nil {
		events.Error(llm.NewErrStreamRead(llm.ProviderNameBedrock, err))
	}
}
