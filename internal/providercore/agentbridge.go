package providercore

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	agentunified "github.com/codewandler/agentapis/api/unified"
	agentclient "github.com/codewandler/agentapis/client"
	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

type llmBridgeBuilder struct {
	cfg            clientConfig
	originalReq    llm.Request
	resolvedReq    llm.Request
	requestedModel string
	resolvedAPI    llm.ApiType
}

func (b llmBridgeBuilder) NewBridge() agentclient.StreamBridge[llm.Request, llm.Event] {
	collector := &collectingPublisher{}
	var publisher llm.Publisher = collector
	if b.resolvedAPI == llm.ApiTypeOpenAIChatCompletion {
		publisher = newCompletionsToolAccumulator(collector)
	}
	return &llmBridge{
		cfg:            b.cfg,
		originalReq:    b.originalReq,
		resolvedReq:    b.resolvedReq,
		requestedModel: b.requestedModel,
		resolvedAPI:    b.resolvedAPI,
		collector:      collector,
		publisher:      publisher,
	}
}

type llmBridge struct {
	cfg            clientConfig
	originalReq    llm.Request
	resolvedReq    llm.Request
	requestedModel string
	resolvedAPI    llm.ApiType

	collector *collectingPublisher
	publisher llm.Publisher

	requestID      string
	responseModel  string
	allTokens      usage.TokenItems
	inputTokens    usage.TokenItems
	outputTokens   usage.TokenItems
	stopReason     llm.StopReason
	startedOnce    bool
	sawToolUseLike bool
	rateLimits     *llm.RateLimits
	usageExtras    map[string]any
}

func (b *llmBridge) BuildRequest(_ context.Context, _ llm.Request) (agentunified.Request, agentclient.UpstreamHints, error) {
	uReq, err := requestToAgentUnified(b.resolvedReq)
	if err != nil {
		return agentunified.Request{}, agentclient.UpstreamHints{}, err
	}
	target := apiTypeToTarget(b.resolvedAPI)
	return uReq, agentclient.UpstreamHints{PreferredTarget: &target}, nil
}

func (b *llmBridge) OnRequest(_ context.Context, meta agentclient.RequestMeta) ([]llm.Event, error) {
	var out []llm.Event
	if b.requestedModel != "" && b.requestedModel != b.resolvedReq.Model {
		out = append(out, &llm.ModelResolvedEvent{
			Resolver: b.cfg.ProviderName,
			Name:     b.requestedModel,
			Resolved: b.resolvedReq.Model,
		})
	}
	out = append(out, &llm.RequestEvent{
		OriginalRequest: b.originalReq,
		ProviderRequest: llm.ProviderRequestFromHTTP(meta.HTTP, meta.Body),
		ResolvedApiType: b.resolvedAPI,
	})
	return out, nil
}

func (b *llmBridge) OnResponse(_ context.Context, meta agentclient.ResponseMeta) ([]llm.Event, error) {
	resp := &http.Response{StatusCode: meta.StatusCode, Header: meta.Headers}
	if b.cfg.RateLimitParser != nil {
		b.rateLimits = b.cfg.RateLimitParser(resp)
	}
	if b.cfg.UsageExtras != nil {
		b.usageExtras = b.cfg.UsageExtras(resp)
	}
	return nil, nil
}

func (b *llmBridge) OnEvent(_ context.Context, ev agentunified.StreamEvent) ([]llm.Event, error) {
	switch b.resolvedAPI {
	case llm.ApiTypeAnthropicMessages:
		return b.onMessagesEvent(ev)
	case llm.ApiTypeOpenAIResponses:
		return b.onResponsesEvent(ev)
	default:
		return b.onCompletionsEvent(ev)
	}
}

func (b *llmBridge) OnClose(_ context.Context) ([]llm.Event, error) {
	switch b.resolvedAPI {
	case llm.ApiTypeAnthropicMessages:
		emitUsageRecord(b.publisher, b.cfg.ProviderName, b.resolvedReq.Model, b.requestID, b.responseModel, append(b.inputTokens, b.outputTokens...).NonZero(), b.rateLimits, b.usageExtras)
	case llm.ApiTypeOpenAIResponses:
		stop := b.stopReason
		if stop == llm.StopReasonEndTurn && b.sawToolUseLike {
			stop = llm.StopReasonToolUse
		}
		emitUsageRecord(b.publisher, b.cfg.ProviderName, b.resolvedReq.Model, b.requestID, b.responseModel, b.allTokens.NonZero(), b.rateLimits, b.usageExtras)
		b.publisher.Completed(llm.CompletedEvent{StopReason: stop})
		return b.collector.Take(), nil
	default:
		emitUsageRecord(b.publisher, b.cfg.ProviderName, b.resolvedReq.Model, b.requestID, b.responseModel, b.allTokens.NonZero(), b.rateLimits, b.usageExtras)
	}
	b.publisher.Completed(llm.CompletedEvent{StopReason: b.stopReason})
	return b.collector.Take(), nil
}

func (b *llmBridge) onMessagesEvent(ev agentunified.StreamEvent) ([]llm.Event, error) {
	var out []llm.Event
	if ev.Started != nil {
		b.requestID = ev.Started.RequestID
		if ev.Started.Model != "" {
			b.responseModel = ev.Started.Model
		}
		if ev.Started.Model != "" && ev.Started.Model != b.resolvedReq.Model {
			out = append(out, &llm.ModelResolvedEvent{Resolver: b.cfg.ProviderName, Name: b.resolvedReq.Model, Resolved: ev.Started.Model})
		}
		ev.Started.Provider = b.cfg.ProviderName
		if b.rateLimits != nil {
			ev.Started.Extra = map[string]any{"rate_limits": b.rateLimits}
		}
	}
	if ev.Started != nil && ev.Usage != nil {
		b.inputTokens = agentInputTokensToUsage(ev.Usage.Input)
		ev.Usage = nil
	}
	if ev.Completed != nil {
		if ev.Usage != nil {
			b.outputTokens = agentOutputTokensToUsage(ev.Usage.Output)
			ev.Usage = nil
		}
		b.stopReason = llm.StopReason(ev.Completed.StopReason)
		ev.Completed = nil
	}
	if err := publishAgentUnifiedToLLM(b.publisher, ev); err != nil {
		return nil, err
	}
	return append(out, b.collector.Take()...), nil
}

func (b *llmBridge) onCompletionsEvent(ev agentunified.StreamEvent) ([]llm.Event, error) {
	var out []llm.Event
	if ev.Started != nil && !b.startedOnce {
		b.startedOnce = true
		b.requestID = ev.Started.RequestID
		if ev.Started.Model != "" {
			b.responseModel = ev.Started.Model
		}
		if ev.Started.Model != "" && ev.Started.Model != b.resolvedReq.Model {
			out = append(out, &llm.ModelResolvedEvent{Resolver: b.cfg.ProviderName, Name: b.resolvedReq.Model, Resolved: ev.Started.Model})
		}
		ev.Started.Provider = b.cfg.ProviderName
		if b.rateLimits != nil {
			ev.Started.Extra = map[string]any{"rate_limits": b.rateLimits}
		}
	} else if ev.Started != nil {
		ev.Started = nil
	}
	if ev.Usage != nil {
		b.allTokens = append(b.allTokens, agentInputTokensToUsage(ev.Usage.Input)...)
		b.allTokens = append(b.allTokens, agentOutputTokensToUsage(ev.Usage.Output)...)
		ev.Usage = nil
	}
	if ev.Completed != nil {
		b.stopReason = llm.StopReason(ev.Completed.StopReason)
		ev.Completed = nil
	}
	if err := publishAgentUnifiedToLLM(b.publisher, ev); err != nil {
		return nil, err
	}
	return append(out, b.collector.Take()...), nil
}

func (b *llmBridge) onResponsesEvent(ev agentunified.StreamEvent) ([]llm.Event, error) {
	var out []llm.Event
	if ev.Started != nil {
		b.requestID = ev.Started.RequestID
		if ev.Started.Model != "" {
			b.responseModel = ev.Started.Model
		}
		if ev.Started.Model != "" && ev.Started.Model != b.resolvedReq.Model {
			out = append(out, &llm.ModelResolvedEvent{Resolver: b.cfg.ProviderName, Name: b.resolvedReq.Model, Resolved: ev.Started.Model})
		}
		ev.Started.Provider = b.cfg.ProviderName
		if b.rateLimits != nil {
			ev.Started.Extra = map[string]any{"rate_limits": b.rateLimits}
		}
	}
	if ev.ToolDelta != nil || ev.StreamToolCall != nil || ev.ToolCall != nil {
		b.sawToolUseLike = true
	}
	if ev.Usage != nil {
		b.allTokens = append(b.allTokens, agentInputTokensToUsage(ev.Usage.Input)...)
		b.allTokens = append(b.allTokens, agentOutputTokensToUsage(ev.Usage.Output)...)
		ev.Usage = nil
	}
	if ev.Completed != nil {
		b.stopReason = llm.StopReason(ev.Completed.StopReason)
		ev.Completed = nil
	}
	if err := publishAgentUnifiedToLLM(b.publisher, ev); err != nil {
		return nil, err
	}
	return append(out, b.collector.Take()...), nil
}

type collectingPublisher struct{ events []llm.Event }

func (p *collectingPublisher) Publish(payload llm.Event)              { p.events = append(p.events, payload) }
func (p *collectingPublisher) Started(started llm.StreamStartedEvent) { p.Publish(&started) }
func (p *collectingPublisher) ModelResolved(resolver, name, resolved string) {
	p.Publish(&llm.ModelResolvedEvent{Resolver: resolver, Name: name, Resolved: resolved})
}
func (p *collectingPublisher) Failover(from, to string, err error) {
	p.Publish(&llm.ProviderFailoverEvent{Provider: from, FailoverProvider: to, Error: err})
}
func (p *collectingPublisher) Delta(d *llm.DeltaEvent)               { p.Publish(d) }
func (p *collectingPublisher) ToolCall(tc tool.Call)                 { p.Publish(&llm.ToolCallEvent{ToolCall: tc}) }
func (p *collectingPublisher) ContentBlock(evt llm.ContentPartEvent) { p.Publish(&evt) }
func (p *collectingPublisher) UsageRecord(r usage.Record) {
	p.Publish(&llm.UsageUpdatedEvent{Record: r})
}
func (p *collectingPublisher) TokenEstimate(r usage.Record) {
	p.Publish(&llm.TokenEstimateEvent{Estimate: r})
}
func (p *collectingPublisher) Completed(completed llm.CompletedEvent) { p.Publish(&completed) }
func (p *collectingPublisher) Error(err error)                        { p.Publish(&llm.ErrorEvent{Error: err}) }
func (p *collectingPublisher) Debug(msg string, data any) {
	p.Publish(&llm.DebugEvent{Message: msg, Data: data})
}
func (p *collectingPublisher) Close() {}
func (p *collectingPublisher) Take() []llm.Event {
	out := append([]llm.Event(nil), p.events...)
	p.events = nil
	return out
}

type completionsToolAccumulator struct {
	llm.Publisher
	active map[uint32]*accumulatedCompletionTool
}

type accumulatedCompletionTool struct {
	id   string
	name string
	args strings.Builder
}

func newCompletionsToolAccumulator(base llm.Publisher) *completionsToolAccumulator {
	return &completionsToolAccumulator{Publisher: base, active: make(map[uint32]*accumulatedCompletionTool)}
}

func (p *completionsToolAccumulator) Delta(ev *llm.DeltaEvent) {
	if ev != nil && ev.Kind == llm.DeltaKindTool && ev.Index != nil {
		idx := *ev.Index
		acc := p.active[idx]
		if acc == nil {
			acc = &accumulatedCompletionTool{}
			p.active[idx] = acc
		}
		if ev.ToolID != "" {
			acc.id = ev.ToolID
		}
		if ev.ToolName != "" {
			acc.name = ev.ToolName
		}
		if ev.ToolArgs != "" {
			acc.args.WriteString(ev.ToolArgs)
		}
	}
	p.Publisher.Delta(ev)
}

func (p *completionsToolAccumulator) Completed(ev llm.CompletedEvent) {
	if ev.StopReason == llm.StopReasonToolUse {
		p.flushToolCalls()
	}
	p.Publisher.Completed(ev)
}

func (p *completionsToolAccumulator) Close() {
	p.flushToolCalls()
	p.Publisher.Close()
}

func (p *completionsToolAccumulator) flushToolCalls() {
	if len(p.active) == 0 {
		return
	}
	indices := make([]uint32, 0, len(p.active))
	for idx := range p.active {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	for _, idx := range indices {
		acc := p.active[idx]
		if acc == nil {
			continue
		}
		if acc.args.Len() == 0 && acc.id == "" && acc.name == "" {
			continue
		}
		var args map[string]any
		if acc.args.Len() > 0 {
			if err := json.Unmarshal([]byte(acc.args.String()), &args); err != nil {
				args = map[string]any{"_raw": acc.args.String()}
			}
		}
		p.Publisher.ToolCall(tool.NewToolCall(acc.id, acc.name, args))
	}
	p.active = make(map[uint32]*accumulatedCompletionTool)
}

func publishAgentUnifiedToLLM(pub llm.Publisher, ev agentunified.StreamEvent) error {
	handled := false
	if ev.Started != nil {
		handled = true
		pub.Started(llm.StreamStartedEvent{RequestID: ev.Started.RequestID, Model: ev.Started.Model, Provider: ev.Started.Provider, Extra: cloneAnyMap(ev.Started.Extra)})
	}
	if ev.Delta != nil {
		handled = true
		pub.Delta(&llm.DeltaEvent{Kind: llm.DeltaKind(ev.Delta.Kind), Index: ev.Delta.Index, Text: ev.Delta.Text, Thinking: ev.Delta.Thinking, ToolDeltaPart: llm.ToolDeltaPart{ToolID: ev.Delta.ToolID, ToolName: ev.Delta.ToolName, ToolArgs: ev.Delta.ToolArgs}})
	}
	if ev.ToolCall != nil {
		handled = true
		pub.ToolCall(tool.NewToolCall(ev.ToolCall.ID, ev.ToolCall.Name, cloneAnyMap(ev.ToolCall.Args)))
	}
	if ev.Content != nil {
		handled = true
		pub.ContentBlock(llm.ContentPartEvent{Part: agentPartToMsgPart(ev.Content.Part), Index: ev.Content.Index})
	}
	if ev.Usage != nil {
		handled = true
		tokens := append(agentInputTokensToUsage(ev.Usage.Input), agentOutputTokensToUsage(ev.Usage.Output)...).NonZero()
		pub.UsageRecord(usage.Record{Dims: usage.Dims{Provider: ev.Usage.Provider, Model: ev.Usage.Model, RequestID: ev.Usage.RequestID}, Tokens: tokens, Cost: agentCostsToUsageCost(ev.Usage.Costs), RecordedAt: ev.Usage.RecordedAt, Extras: cloneAnyMap(ev.Usage.Extras)})
	}
	if ev.Completed != nil {
		handled = true
		pub.Completed(llm.CompletedEvent{StopReason: llm.StopReason(ev.Completed.StopReason)})
	}
	if ev.Error != nil && ev.Error.Err != nil {
		handled = true
		pub.Error(ev.Error.Err)
	}
	if hasUnprojectedSemanticPayload(ev) || (!handled && (ev.Extras.RawEventName != "" || len(ev.Extras.RawJSON) > 0)) {
		pub.Debug("unified.stream_event", ev)
	}
	return nil
}

func hasUnprojectedSemanticPayload(ev agentunified.StreamEvent) bool {
	return ev.Lifecycle != nil || ev.ContentDelta != nil || ev.StreamContent != nil || ev.ToolDelta != nil || ev.StreamToolCall != nil || ev.Annotation != nil || ev.Type == agentunified.StreamEventUnknown
}

func emitUsageRecord(pub llm.Publisher, provider, model, requestID, responseModel string, tokens usage.TokenItems, rateLimits *llm.RateLimits, extras map[string]any) {
	if len(tokens) == 0 {
		return
	}
	rec := usage.Record{Dims: usage.Dims{Provider: provider, Model: model, RequestID: requestID}, Tokens: tokens, RecordedAt: time.Now(), Extras: cloneAnyMap(extras)}
	if cost, ok := usage.Default().Calculate(provider, chooseModel(responseModel, model), tokens); ok {
		rec.Cost = cost
	}
	if rateLimits != nil {
		if rec.Extras == nil {
			rec.Extras = make(map[string]any)
		}
		rec.Extras["rate_limits"] = rateLimits
	}
	pub.UsageRecord(rec)
}

func chooseModel(responseModel, fallback string) string {
	if responseModel != "" {
		return responseModel
	}
	return fallback
}

func requestToAgentUnified(req llm.Request) (agentunified.Request, error) {
	if err := req.Validate(); err != nil {
		return agentunified.Request{}, err
	}
	out := agentunified.Request{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
		Effort:      agentunified.Effort(req.Effort),
		Thinking:    agentunified.ThinkingMode(req.Thinking),
		CacheHint:   convertCacheHint(req.CacheHint),
		ToolChoice:  convertToolChoice(req.ToolChoice),
	}
	if req.OutputFormat != "" {
		switch req.OutputFormat {
		case llm.OutputFormatText:
			out.Output = &agentunified.OutputSpec{Mode: agentunified.OutputModeText}
		case llm.OutputFormatJSON:
			out.Output = &agentunified.OutputSpec{Mode: agentunified.OutputModeJSONObject}
		}
	}
	if req.RequestMeta != nil {
		out.Metadata = &agentunified.RequestMetadata{User: req.RequestMeta.User, Metadata: cloneAnyMap(req.RequestMeta.Metadata)}
	}
	if len(req.Tools) > 0 {
		out.Tools = make([]agentunified.Tool, 0, len(req.Tools))
		for _, t := range req.Tools {
			out.Tools = append(out.Tools, agentunified.Tool{Name: t.Name, Description: t.Description, Parameters: cloneAnyMap(t.Parameters)})
		}
	}
	if len(req.Messages) > 0 {
		out.Messages = make([]agentunified.Message, 0, len(req.Messages))
		for _, m := range req.Messages {
			out.Messages = append(out.Messages, convertMessage(m))
		}
	}
	return out, nil
}

func convertMessage(in msg.Message) agentunified.Message {
	out := agentunified.Message{Role: agentunified.Role(in.Role), Phase: agentunified.AssistantPhase(in.Phase), CacheHint: convertCacheHint(in.CacheHint)}
	if len(in.Parts) > 0 {
		out.Parts = make([]agentunified.Part, 0, len(in.Parts))
		for _, part := range in.Parts {
			out.Parts = append(out.Parts, convertPart(part))
		}
	}
	return out
}

func convertPart(in msg.Part) agentunified.Part {
	out := agentunified.Part{Type: agentunified.PartType(in.Type), Text: in.Text}
	if in.Thinking != nil {
		out.Thinking = &agentunified.ThinkingPart{Provider: in.Thinking.Provider, Text: in.Thinking.Text, Signature: in.Thinking.Signature}
	}
	if in.ToolCall != nil {
		out.ToolCall = &agentunified.ToolCall{ID: in.ToolCall.ID, Name: in.ToolCall.Name, Args: cloneAnyMap(in.ToolCall.Args)}
	}
	if in.ToolResult != nil {
		out.ToolResult = &agentunified.ToolResult{ToolCallID: in.ToolResult.ToolCallID, ToolOutput: in.ToolResult.ToolOutput, IsError: in.ToolResult.IsError}
	}
	return out
}

func convertToolChoice(choice llm.ToolChoice) agentunified.ToolChoice {
	switch tc := choice.(type) {
	case nil:
		return nil
	case llm.ToolChoiceAuto:
		return agentunified.ToolChoiceAuto{}
	case llm.ToolChoiceRequired:
		return agentunified.ToolChoiceRequired{}
	case llm.ToolChoiceNone:
		return agentunified.ToolChoiceNone{}
	case llm.ToolChoiceTool:
		return agentunified.ToolChoiceTool{Name: tc.Name}
	default:
		return nil
	}
}

func convertCacheHint(in *msg.CacheHint) *agentunified.CacheHint {
	if in == nil {
		return nil
	}
	return &agentunified.CacheHint{Enabled: in.Enabled, TTL: in.TTL}
}

func agentPartToMsgPart(part agentunified.Part) msg.Part {
	out := msg.Part{Type: msg.PartType(part.Type), Text: part.Text}
	if part.Thinking != nil {
		out.Thinking = &msg.ThinkingPart{Provider: part.Thinking.Provider, Text: part.Thinking.Text, Signature: part.Thinking.Signature}
	}
	if part.ToolCall != nil {
		out.ToolCall = &msg.ToolCall{ID: part.ToolCall.ID, Name: part.ToolCall.Name, Args: cloneAnyMap(part.ToolCall.Args)}
	}
	if part.ToolResult != nil {
		out.ToolResult = &msg.ToolResult{ToolCallID: part.ToolResult.ToolCallID, ToolOutput: part.ToolResult.ToolOutput, IsError: part.ToolResult.IsError}
	}
	return out
}

func agentInputTokensToUsage(in agentunified.InputTokens) usage.TokenItems {
	return usage.TokenItems{
		{Kind: usage.KindInput, Count: in.New},
		{Kind: usage.KindCacheRead, Count: in.CacheRead},
		{Kind: usage.KindCacheWrite, Count: in.CacheWrite},
	}.NonZero()
}

func agentOutputTokensToUsage(in agentunified.OutputTokens) usage.TokenItems {
	output := in.Total - in.Reasoning
	if output < 0 {
		output = 0
	}
	return usage.TokenItems{
		{Kind: usage.KindOutput, Count: output},
		{Kind: usage.KindReasoning, Count: in.Reasoning},
	}.NonZero()
}

func agentCostsToUsageCost(in agentunified.CostItems) usage.Cost {
	var total float64
	for _, item := range in {
		total += item.Amount
	}
	return usage.Cost{Total: total, Input: in.ByKind(agentunified.CostKindInput), Output: in.ByKind(agentunified.CostKindOutput), Reasoning: in.ByKind(agentunified.CostKindReasoning), CacheRead: in.ByKind(agentunified.CostKindInputCacheRead), CacheWrite: in.ByKind(agentunified.CostKindInputCacheWrite), Source: "calculated"}
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func apiTypeToTarget(api llm.ApiType) agentclient.Target {
	switch api {
	case llm.ApiTypeAnthropicMessages:
		return agentclient.TargetMessages
	case llm.ApiTypeOpenAIResponses:
		return agentclient.TargetResponses
	default:
		return agentclient.TargetCompletions
	}
}
