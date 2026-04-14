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
	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// Environment variable names for AWS configuration.
const (
	EnvAWSAccessKeyID     = "AWS_ACCESS_KEY_ID"
	EnvAWSSecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	EnvAWSRegion          = "AWS_REGION"
	EnvAWSDefaultRegion   = "AWS_DEFAULT_REGION"
	EnvAWSProfile         = "AWS_PROFILE"
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

// CostCalculator returns the default cost calculator for Bedrock.
func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

// DefaultModel returns the configured default model ID.
func (p *Provider) DefaultModel() string {
	return p.defaultModel
}

// Models returns a curated list of popular Bedrock models.
func (p *Provider) Models() llm.Models                      { return models() }
func (p *Provider) Resolve(model string) (llm.Model, error) { return p.Models().Resolve(model) }

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

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameBedrock, err)
	}

	// Lazy client initialization (thread-safe)
	if err := p.initClient(ctx); err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameBedrock, err)
	}

	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameBedrock, err)
	}

	// Resolve model to include inference profile prefix
	resolvedModel, err := p.resolveModel(opts.Model)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameBedrock, err)
	}

	// Create a copy of opts with resolved model
	resolvedOpts := opts
	resolvedOpts.Model = resolvedModel

	input, err := buildRequest(resolvedOpts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameBedrock, err)
	}

	output, err := p.client.ConverseStream(ctx, input)
	if err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameBedrock, err)
	}

	meta := streamMeta{
		RequestedModel: opts.Model,
		ResolvedModel:  resolvedModel,
		Logger:         p.logger,
		RequestID:      gonanoid.Must(),
	}
	pub, ch := llm.NewEventPublisher()

	// Emit token estimates (primary + per-segment breakdown)
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, llm.ProviderNameBedrock, opts.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	go parseStream(ctx, output, pub, meta)
	return ch, nil
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
	for _, m := range msgs {
		switch m.Role {
		case msg.RoleSystem, msg.RoleUser, msg.RoleAssistant, msg.RoleTool:
			if m.CacheHint != nil && m.CacheHint.Enabled {
				return true
			}
		}
	}
	return false
}

func buildRequest(opts llm.Request) (*bedrockruntime.ConverseStreamInput, error) {
	input := &bedrockruntime.ConverseStreamInput{
		ModelId: aws.String(opts.Model),
	}

	// Convert messages
	var system []types.SystemContentBlock
	var messages []types.Message

	for idx, m := range opts.Messages {
		switch m.Role {
		case msg.RoleSystem:
			system = append(system, &types.SystemContentBlockMemberText{
				Value: m.Text(),
			})
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				system = append(system, &types.SystemContentBlockMemberCachePoint{
					Value: *cp,
				})
			}

		case msg.RoleUser:
			content := []types.ContentBlock{
				&types.ContentBlockMemberText{Value: m.Text()},
			}
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				content = append(content, &types.ContentBlockMemberCachePoint{Value: *cp})
			}
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleUser,
				Content: content,
			})

		case msg.RoleAssistant:
			var content []types.ContentBlock
			for _, p := range m.Parts {
				switch p.Type {
				case msg.PartTypeText:
					// Individual text blocks preserve interleaved position
					// relative to thinking blocks.
					if p.Text != "" {
						content = append(content, &types.ContentBlockMemberText{Value: p.Text})
					}
				case msg.PartTypeThinking:
					if p.Thinking == nil {
						continue
					}
					content = append(content, &types.ContentBlockMemberReasoningContent{
						Value: &types.ReasoningContentBlockMemberReasoningText{
							Value: types.ReasoningTextBlock{
								Text:      aws.String(p.Thinking.Text),
								Signature: aws.String(p.Thinking.Signature),
							},
						},
					})
				case msg.PartTypeToolCall:
					tc := p.ToolCall
					inputDoc, err := toDocument(tc.Args)
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
			}
			if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
				content = append(content, &types.ContentBlockMemberCachePoint{Value: *cp})
			}
			messages = append(messages, types.Message{
				Role:    types.ConversationRoleAssistant,
				Content: content,
			})

		case msg.RoleTool:
			var toolResults []types.ContentBlock
			// Collect consecutive tool results
			for _, tr := range opts.Messages[idx+1:] {
				if tr.Role != msg.RoleTool {
					break
				}
				status := types.ToolResultStatusSuccess
				if tr.ToolResults()[0].IsError {
					status = types.ToolResultStatusError
				}
				toolResults = append(toolResults, &types.ContentBlockMemberToolResult{
					Value: types.ToolResultBlock{
						ToolUseId: aws.String(tr.ToolResults()[0].ToolCallID),
						Content: []types.ToolResultContentBlock{
							&types.ToolResultContentBlockMemberText{Value: tr.ToolResults()[0].ToolOutput},
						},
						Status: status,
					},
				})
			}
			// Add cache point after tool results if present
			if m.CacheHint != nil && m.CacheHint.Enabled {
				if cp := buildBedrockCachePoint(m.CacheHint, opts.Model); cp != nil {
					toolResults = append(toolResults, &types.ContentBlockMemberCachePoint{Value: *cp})
				}
			}
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
			schema, err := toDocument(sortmap.NewSortedMap(t.Parameters))
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

		// Set tool choice — force-tool is incompatible with extended thinking.
		// Fall back to auto when reasoning will be enabled.
		effectiveToolChoice := opts.ToolChoice
		if opts.Thinking.IsOn() || (!opts.Thinking.IsOff() && !opts.Effort.IsEmpty()) {
			if _, isForced := effectiveToolChoice.(llm.ToolChoiceTool); isForced {
				effectiveToolChoice = llm.ToolChoiceAuto{}
			}
		}
		switch tc := effectiveToolChoice.(type) {
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

	// Build additional model request fields for parameters not in inferenceConfig.
	// Some models support topK and other extended parameters via additional fields.
	// We merge these with reasoning_config if both are specified.
	var additionalFields map[string]any

	// Some models support topK via additional request fields.
	// Note: Not all Bedrock models support topK.
	if opts.TopK > 0 {
		if additionalFields == nil {
			additionalFields = make(map[string]any)
		}
		additionalFields["top_k"] = opts.TopK
	}

	// Some Bedrock models support output_format via additional request fields.
	// This is passed as {"outputSchema": {...}} for models that support it.
	if opts.OutputFormat == llm.OutputFormatJSON {
		if additionalFields == nil {
			additionalFields = make(map[string]any)
		}
		additionalFields["output_schema"] = map[string]any{
			"type": "json_object",
		}
	}

	// Wire reasoning/thinking via additionalModelRequestFields.
	// Bedrock uses reasoning_config: {type: "enabled", budget_tokens: N}.
	// ThinkingOff → omit reasoning_config entirely.
	// ThinkingOn → always enable reasoning.
	// ThinkingAuto → only enable reasoning when Effort is explicitly set
	//   (avoid cost increase from always-on reasoning with no user intent).
	if opts.Thinking.IsOn() || (!opts.Thinking.IsOff() && !opts.Effort.IsEmpty()) {
		budget := 31999 // default when no effort specified
		if b, ok := opts.Effort.ToBudget(1024, 31999); ok {
			budget = b
		}
		if additionalFields == nil {
			additionalFields = make(map[string]any)
		}
		additionalFields["reasoning_config"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": budget,
		}
	}

	// Enable interleaved thinking beta for Claude models.
	// Harmless no-op for models that don't support it.
	if isClaudeModel(opts.Model) {
		if additionalFields == nil {
			additionalFields = make(map[string]any)
		}
		additionalFields["anthropic_beta"] = []string{anthropic.BetaInterleavedThinking}
	}

	// Set additional fields if any were collected
	if len(additionalFields) > 0 {
		fieldsDoc, err := toDocument(additionalFields)
		if err != nil {
			return nil, fmt.Errorf("marshal additional request fields: %w", err)
		}
		input.AdditionalModelRequestFields = fieldsDoc
	}

	// Set inference configuration (temperature, topP, maxTokens).
	// Only set when at least one parameter is configured.
	if opts.Temperature > 0 || opts.TopP > 0 || opts.MaxTokens > 0 {
		inferenceConfig := &types.InferenceConfiguration{}
		if opts.MaxTokens > 0 {
			inferenceConfig.MaxTokens = aws.Int32(int32(opts.MaxTokens))
		}
		if opts.Temperature > 0 {
			inferenceConfig.Temperature = aws.Float32(float32(opts.Temperature))
		}
		if opts.TopP > 0 {
			inferenceConfig.TopP = aws.Float32(float32(opts.TopP))
		}
		input.InferenceConfig = inferenceConfig
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

// --- Publisher parsing ---

// streamMeta passes context into the stream parser for StreamEventStart.
type streamMeta struct {
	RequestedModel string
	ResolvedModel  string
	Logger         *slog.Logger
	RequestID      string // synthesized; Bedrock API does not provide one
}

func parseStream(ctx context.Context, output *bedrockruntime.ConverseStreamOutput, pub llm.Publisher, meta streamMeta) {
	defer pub.Close()

	stream := output.GetStream()
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure is non-fatal
	defer stream.Close()

	logEvent := func(eventType string, v any) {
		if meta.Logger == nil {
			return
		}
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		merged := `{"type":"` + eventType + `"`
		if string(b) != "{}" {
			merged += "," + string(b[1:])
		} else {
			merged += "}"
		}
		meta.Logger.Debug("http response body",
			"method", "POST",
			"url", "bedrock/converse-stream",
			"chunk", "data: "+merged+"\n\n",
		)
	}

	type toolAccum struct {
		id      string
		name    string
		argsBuf strings.Builder
	}
	activeTools := make(map[int]*toolAccum)
	var inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int
	var stopReason llm.StopReason
	startEmitted := false

	for event := range stream.Events() {
		select {
		case <-ctx.Done():
			pub.Error(llm.NewErrContextCancelled(llm.ProviderNameBedrock, ctx.Err()))
			return
		default:
		}

		if !startEmitted {
			startEmitted = true
			pub.Started(llm.StreamStartedEvent{Model: meta.ResolvedModel})
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
					pub.Delta(llm.TextDelta(delta.Value).WithIndex(uint32(idx)))
				case *types.ContentBlockDeltaMemberToolUse:
					if tb, ok := activeTools[idx]; ok && delta.Value.Input != nil {
						tb.argsBuf.WriteString(*delta.Value.Input)
						pub.Delta(llm.ToolDelta(tb.id, tb.name, *delta.Value.Input).WithIndex(uint32(idx)))
					}
				case *types.ContentBlockDeltaMemberReasoningContent:
					switch r := delta.Value.(type) {
					case *types.ReasoningContentBlockDeltaMemberText:
						pub.Delta(llm.ThinkingDelta(r.Value).WithIndex(uint32(idx)))
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
				pub.ToolCall(tool.NewToolCall(tb.id, tb.name, args))
				delete(activeTools, idx)
			}

		case *types.ConverseStreamOutputMemberMetadata:
			logEvent("metadata", e.Value)
			if e.Value.Usage != nil {
				if e.Value.Usage.InputTokens != nil {
					inputTokens = int(*e.Value.Usage.InputTokens)
				}
				if e.Value.Usage.OutputTokens != nil {
					outputTokens = int(*e.Value.Usage.OutputTokens)
				}
				if e.Value.Usage.CacheReadInputTokens != nil {
					cacheReadTokens = int(*e.Value.Usage.CacheReadInputTokens)
				}
				if e.Value.Usage.CacheWriteInputTokens != nil {
					cacheWriteTokens = int(*e.Value.Usage.CacheWriteInputTokens)
				}
				// inputTokens from Bedrock is the non-cache portion.
			}
			tokens := usage.TokenItems{
				{Kind: usage.KindInput, Count: inputTokens},
				{Kind: usage.KindCacheRead, Count: cacheReadTokens},
				{Kind: usage.KindCacheWrite, Count: cacheWriteTokens},
				{Kind: usage.KindOutput, Count: outputTokens},
			}.NonZero()

			// Strip regional inference profile prefix (us., eu., global., etc.)
			// before cost lookup — the pricing table uses bare model IDs.
			costModel := stripRegionPrefix(meta.ResolvedModel)

			rec := usage.Record{
				Dims:       usage.Dims{Provider: llm.ProviderNameBedrock, Model: meta.ResolvedModel, RequestID: meta.RequestID},
				Tokens:     tokens,
				RecordedAt: time.Now(),
			}
			if cost, ok := usage.Default().Calculate(llm.ProviderNameBedrock, costModel, tokens); ok {
				rec.Cost = cost
			}
			pub.UsageRecord(rec)
			pub.Completed(llm.CompletedEvent{StopReason: stopReason})
			return

		case *types.ConverseStreamOutputMemberMessageStop:
			logEvent("message_stop", e.Value)
			stopReason = mapBedrockStopReason(e.Value.StopReason)
		}
	}

	if err := stream.Err(); err != nil {
		pub.Error(llm.NewErrStreamRead(llm.ProviderNameBedrock, err))
	}
}

// mapBedrockStopReason converts the Bedrock SDK StopReason to our typed StopReason.
func mapBedrockStopReason(r types.StopReason) llm.StopReason {
	switch r {
	case types.StopReasonEndTurn:
		return llm.StopReasonEndTurn
	case types.StopReasonToolUse:
		return llm.StopReasonToolUse
	case types.StopReasonMaxTokens:
		return llm.StopReasonMaxTokens
	case types.StopReasonContentFiltered:
		return llm.StopReasonContentFilter
	default:
		return llm.StopReason(r)
	}
}
