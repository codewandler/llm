//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/usage"
)

type integrationScenario struct {
	name    string
	request func(model string) llm.Request
	enabled func(target integrationTarget) (bool, string)
	assert  func(t *testing.T, run integrationRun)
}

func integrationScenarios() []integrationScenario {
	return []integrationScenario{
		{
			name: "plain_text_pong",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 64, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.User("Reply with pong."))}
			},
			enabled: alwaysEnabled,
			assert:  assertTextContains("pong"),
		},
		{
			name: "system_prompt_kiwi",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 64, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.System("Reply with exactly the word kiwi."), msg.User("What should you reply with?"))}
			},
			enabled: alwaysEnabled,
			assert:  assertTextContains("kiwi"),
		},
		{
			name: "cache_not_sent_by_default",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 64, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.System("Reply with exactly the word kiwi."), msg.User("What should you reply with?"))}
			},
			enabled: requiresAnyCaching,
			assert:  assertNoCacheFieldsOnWire("kiwi"),
		},
		{
			name: "cache_explicit_wire_marked",
			request: func(model string) llm.Request {
				_ = model
				return llm.Request{}
			},
			enabled: requiresExplicitCaching,
			assert:  assertExplicitCacheWireMarked("kiwi"),
		},

		{
			name: "cache_usage_effective",
			request: func(model string) llm.Request {
				_ = model
				return llm.Request{}
			},
			enabled: requiresObservableCacheUsage,
			assert:  assertCacheUsageEffective("kiwi"),
		},
		{
			name: "cache_message_overrides_request_level",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 64, Thinking: llm.ThinkingOff, CacheHint: &llm.CacheHint{Enabled: true, TTL: "1h"}, Messages: msg.BuildTranscript(msg.System("Reply with exactly the word kiwi.").Cache(msg.CacheTTL1h), msg.User("What should you reply with?"))}
			},
			enabled: requiresCachePrecedence,
			assert:  assertCacheMessageOverridesRequestLevel("kiwi"),
		},
		{
			name: "effort_high_preserved",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 128, Effort: llm.EffortHigh, Messages: msg.BuildTranscript(msg.User("Reply with exactly the word aurora."))}
			},
			enabled: requiresEffortSupport,
			assert:  assertEffortPreserved("aurora", llm.EffortHigh),
		},
		{
			name: "thinking_off_respected",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 128, Thinking: llm.ThinkingOff, Messages: msg.BuildTranscript(msg.User("Reply with exactly the word ember."))}
			},
			enabled: requiresThinkingToggleSupport,
			assert:  assertThinkingOffRespected("ember"),
		},
		{
			name: "thinking_text_comet",
			request: func(model string) llm.Request {
				return llm.Request{Model: model, MaxTokens: 2048, Thinking: llm.ThinkingOn, Effort: llm.EffortHigh, Messages: msg.BuildTranscript(msg.System("Solve carefully. If reasoning summaries are supported, provide a detailed reasoning summary before the final answer."), msg.User("Solve this carefully. First reason briefly, then give the final answer only as a number. What is 37 * 43 - 17?"))}
			},
			enabled: requiresReasoningSupport,
			assert:  assertReasoningScenario("1574"),
		},
	}
}

func cacheUsageRequestForTarget(target integrationTarget) llm.Request {
	base := llm.Request{Model: target.model, MaxTokens: 64, Thinking: llm.ThinkingOff}
	messages := msg.BuildTranscript(msg.System(cacheProbeBody()), msg.User("Reply with exactly the word kiwi."))
	c := target.supports.Caching
	if c.Configurable {
		base.CacheHint = &llm.CacheHint{Enabled: true, TTL: "1h"}
		messages[0].CacheHint = &msg.CacheHint{Enabled: true, TTL: "1h"}
	}
	base.Messages = messages
	return base
}

func cacheExplicitRequestForTarget(target integrationTarget) llm.Request {
	base := llm.Request{Model: target.model, MaxTokens: 64, Thinking: llm.ThinkingOff}
	if target.supports.Caching.Configurable {
		base.CacheHint = &llm.CacheHint{Enabled: true, TTL: "1h"}
	}
	messages := msg.BuildTranscript(msg.System("Reply with exactly the word kiwi."), msg.User("What should you reply with?"))
	if target.supports.Caching.Configurable {
		messages[0].CacheHint = &msg.CacheHint{Enabled: true, TTL: "1h"}
	}
	base.Messages = messages
	return base
}

func cacheProbeBody() string {
	line := "Caching probe paragraph: repeat this exact sentence to ensure a sufficiently large prompt prefix for provider-side prompt caching to engage reliably across multiple requests."
	return line + "\n\n" + strings.Repeat(line+"\n", 160)
}

func alwaysEnabled(target integrationTarget) (bool, string) { return true, "" }
func requiresReasoningSupport(target integrationTarget) (bool, string) {
	if !target.supports.Reasoning {
		return false, "target does not advertise reasoning support"
	}
	return true, ""
}
func requiresThinkingToggleSupport(target integrationTarget) (bool, string) {
	if !target.supports.ThinkingToggle {
		return false, "target does not advertise thinking toggle support"
	}
	return true, ""
}
func requiresEffortSupport(target integrationTarget) (bool, string) {
	if !target.supports.Effort {
		return false, "target does not advertise effort support"
	}
	return true, ""
}
func requiresAnyCaching(target integrationTarget) (bool, string) {
	if !target.supports.Caching.Available {
		return false, "target does not advertise caching support"
	}
	return true, ""
}
func requiresExplicitCaching(target integrationTarget) (bool, string) {
	c := target.supports.Caching
	if !c.Available {
		return false, "target does not advertise caching support"
	}
	if !c.Configurable {
		return false, "target does not advertise explicit caching controls"
	}
	if !c.RequestLevelCaching && !c.MessageLevelCaching {
		return false, "target does not advertise explicit caching controls"
	}
	return true, ""
}
func requiresCachePrecedence(target integrationTarget) (bool, string) {
	c := target.supports.Caching
	if !(c.RequestLevelCaching && c.MessageLevelCaching && c.MessageOverridesRequest) {
		return false, "message/request cache precedence is not meaningful for this exposure"
	}
	return true, ""
}

func requiresObservableCacheUsage(target integrationTarget) (bool, string) {
	c := target.supports.Caching
	if !c.Available {
		return false, "target does not advertise caching support"
	}
	if target.expect.ServiceID == "claude" {
		return false, "local Claude runtime does not reliably report cache-read usage"
	}
	if target.name == "openrouter_openai_gpt4o_mini" {
		return false, "target does not reliably report cache-read usage for this scenario"
	}
	return true, ""
}

func assertTextContains(want string) func(t *testing.T, run integrationRun) {
	want = strings.ToLower(want)
	return func(t *testing.T, run integrationRun) {
		t.Helper()
		if run.textStreamCount() == 0 {
			t.Fatalf("expected streamed text events, got %s", run.streamSummary())
		}
		text := strings.ToLower(run.result.Text())
		if !strings.Contains(text, want) {
			t.Fatalf("expected processed text to contain %q, got %q (event types %s)", want, run.result.Text(), run.eventTypesString())
		}
		message := run.result.Message()
		if message.Role != msg.RoleAssistant {
			t.Fatalf("processed message role = %q, want %q", message.Role, msg.RoleAssistant)
		}
		if !strings.Contains(strings.ToLower(message.Text()), want) {
			t.Fatalf("expected processed assistant message to contain %q, got %q", want, message.Text())
		}
	}
}

func assertEffortPreserved(wantText string, wantEffort llm.Effort) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(wantText)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if run.requestEvent == nil {
			t.Fatal("expected a request event to inspect wire body")
		}
		body := run.requestEvent.ProviderRequest.Body
		if len(body) == 0 {
			t.Fatal("expected non-empty provider request body")
		}
		var wire struct {
			Reasoning *struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			OutputConfig *struct {
				Effort string `json:"effort"`
			} `json:"output_config"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatalf("unmarshal wire body: %v", err)
		}
		gotEffort := ""
		if wire.Reasoning != nil && wire.Reasoning.Effort != "" {
			gotEffort = wire.Reasoning.Effort
		}
		if gotEffort == "" && wire.OutputConfig != nil && wire.OutputConfig.Effort != "" {
			gotEffort = wire.OutputConfig.Effort
		}
		if gotEffort == "" {
			t.Fatalf("expected effort in wire request body, got: %s", string(body))
		}
		effortOrder := map[string]int{"low": 1, "medium": 2, "high": 3, "xhigh": 4, "max": 4}
		if effortOrder[gotEffort] < effortOrder[string(wantEffort)] {
			t.Fatalf("wire effort = %q is lower than requested %q", gotEffort, wantEffort)
		}
	}
}

func assertReasoningScenario(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if !run.target.supports.Reasoning {
			return
		}
		if run.reasoningStreamCount() > 0 || strings.TrimSpace(run.result.Thought()) != "" {
			return
		}
		if !reasoningRequestedOnWire(run.requestEvent) {
			t.Fatalf("expected reasoning to be requested on wire for %s, got %s", run.target.name, run.streamSummary())
		}
	}
}

func reasoningRequestedOnWire(reqEv *llm.RequestEvent) bool {
	if reqEv == nil || len(reqEv.ProviderRequest.Body) == 0 {
		return false
	}
	var wire struct {
		Reasoning *struct {
			Effort  string `json:"effort"`
			Summary string `json:"summary"`
		} `json:"reasoning"`
		Thinking *struct {
			Type string `json:"type"`
		} `json:"thinking"`
		OutputConfig *struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(reqEv.ProviderRequest.Body, &wire); err != nil {
		return false
	}
	if wire.Reasoning != nil && (wire.Reasoning.Effort != "" || wire.Reasoning.Summary != "") {
		return true
	}
	if wire.Thinking != nil && wire.Thinking.Type != "" && wire.Thinking.Type != "disabled" {
		return true
	}
	if wire.OutputConfig != nil && wire.OutputConfig.Effort != "" {
		return true
	}
	return false
}

func assertThinkingOffRespected(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if run.reasoningStreamCount() > 0 || strings.TrimSpace(run.result.Thought()) != "" {
			t.Fatalf("expected no reasoning output when thinking is disabled, got %s", run.streamSummary())
		}
	}
}

type cacheWireSummary struct {
	HasTopLevelCacheControl bool
	SystemCachedCount       int
	MessageCachedCount      int
	PartCachedCount         int
	PromptCacheRetention    string
}

func (s cacheWireSummary) HasAnyRequestLevelCache() bool {
	return s.HasTopLevelCacheControl || s.PromptCacheRetention != ""
}

func (s cacheWireSummary) HasAnyMessageLevelCache() bool {
	return s.SystemCachedCount+s.MessageCachedCount+s.PartCachedCount > 0
}

func cacheMarkersOnWire(reqEv *llm.RequestEvent) cacheWireSummary {
	if reqEv == nil || len(reqEv.ProviderRequest.Body) == 0 {
		return cacheWireSummary{}
	}
	var payload map[string]any
	if err := json.Unmarshal(reqEv.ProviderRequest.Body, &payload); err != nil {
		return cacheWireSummary{}
	}
	summary := cacheWireSummary{}
	if _, ok := payload["cache_control"]; ok {
		summary.HasTopLevelCacheControl = true
	}
	if system, ok := payload["system"].([]any); ok {
		for _, item := range system {
			if block, ok := item.(map[string]any); ok {
				if _, ok := block["cache_control"]; ok {
					summary.SystemCachedCount++
				}
			}
		}
	}
	if retention, ok := payload["prompt_cache_retention"].(string); ok {
		summary.PromptCacheRetention = retention
	}
	if messages, ok := payload["messages"].([]any); ok {
		for _, item := range messages {
			msgMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := msgMap["cache_control"]; ok {
				summary.MessageCachedCount++
			}
			if content, ok := msgMap["content"].([]any); ok {
				for _, part := range content {
					if partMap, ok := part.(map[string]any); ok {
						if _, ok := partMap["cache_control"]; ok {
							summary.PartCachedCount++
						}
					}
				}
			}
		}
	}
	return summary
}

func cacheRequestedOnWire(reqEv *llm.RequestEvent) bool {
	s := cacheMarkersOnWire(reqEv)
	return s.HasAnyRequestLevelCache() || s.HasAnyMessageLevelCache()
}

func assertNoCacheFieldsOnWire(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if !run.target.supports.Caching.Available {
			return
		}
		s := cacheMarkersOnWire(run.requestEvent)
		if s.HasAnyRequestLevelCache() || s.HasAnyMessageLevelCache() {
			t.Fatalf("expected no cache markers on wire for %s, contract=%s summary=%+v body=%s", run.target.name, run.target.supports.Caching.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
		}
	}
}

func assertCacheUsageEffective(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		if run.followup == nil {
			t.Fatalf("expected followup run for cache usage scenario")
		}
		textAssert(t, *run.followup)
		first := run.latestUsageRecord()
		second := run.followup.latestUsageRecord()
		if first == nil || second == nil {
			t.Fatalf("expected usage records for both cache usage requests, first=%v second=%v", first, second)
		}
		if first.Tokens.TotalOutput() <= 0 || second.Tokens.TotalOutput() <= 0 {
			t.Fatalf("expected output tokens on both requests, first=%+v second=%+v", first.Tokens, second.Tokens)
		}
		firstRead := first.Tokens.Count(usage.KindCacheRead)
		secondRead := second.Tokens.Count(usage.KindCacheRead)
		firstWrite := first.Tokens.Count(usage.KindCacheWrite)
		secondWrite := second.Tokens.Count(usage.KindCacheWrite)
		if firstRead > 0 || secondRead > 0 {
			return
		}
		if run.target.supports.Caching.ImplicitOnly {
			t.Skipf("target does not reliably expose implicit cache-read usage counters: first=%+v second=%+v", first.Tokens, second.Tokens)
		}
		if firstWrite > 0 || secondWrite > 0 {
			t.Skipf("target reported cache writes but no cache reads across the probe pair: first=%+v second=%+v", first.Tokens, second.Tokens)
		}
		t.Fatalf("expected cache-related usage effect across repeated requests, first=%+v second=%+v", first.Tokens, second.Tokens)
	}
}

func assertImplicitCachingAvailableOnWire(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		c := run.target.supports.Caching
		if !c.ImplicitOnly {
			t.Fatalf("implicit-caching assertion called for non-implicit target: %s contract=%s", run.target.name, c.Summary())
		}
		s := cacheMarkersOnWire(run.requestEvent)
		if s.HasAnyRequestLevelCache() || s.HasAnyMessageLevelCache() {
			t.Fatalf("expected no explicit cache markers on wire for implicit-caching target %s, contract=%s summary=%+v body=%s", run.target.name, c.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
		}
	}
}

func assertExplicitCacheWireMarked(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		c := run.target.supports.Caching
		s := cacheMarkersOnWire(run.requestEvent)
		switch {
		case c.MessageLevelCaching && c.MessageOverridesRequest:
			if !s.HasAnyMessageLevelCache() {
				t.Fatalf("expected message-level cache markers for %s, contract=%s summary=%+v body=%s", run.target.name, c.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
			}
		case c.MessageLevelCaching && !c.RequestLevelCaching:
			if !s.HasAnyMessageLevelCache() {
				t.Fatalf("expected message-level cache markers for %s, contract=%s summary=%+v body=%s", run.target.name, c.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
			}
		case c.RequestLevelCaching:
			if !s.HasAnyRequestLevelCache() && !s.HasAnyMessageLevelCache() {
				t.Fatalf("expected cache markers for %s, contract=%s summary=%+v body=%s", run.target.name, c.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
			}
		default:
			t.Fatalf("target %s enabled explicit cache scenario without explicit caching contract: %s", run.target.name, c.Summary())
		}
	}
}

func assertCacheMessageOverridesRequestLevel(want string) func(t *testing.T, run integrationRun) {
	textAssert := assertTextContains(want)
	return func(t *testing.T, run integrationRun) {
		textAssert(t, run)
		c := run.target.supports.Caching
		s := cacheMarkersOnWire(run.requestEvent)
		if !c.MessageOverridesRequest {
			t.Fatalf("precedence assertion called for target without precedence support: %s contract=%s", run.target.name, c.Summary())
		}
		if !s.HasAnyMessageLevelCache() {
			t.Fatalf("expected message-level cache markers for %s, contract=%s summary=%+v body=%s", run.target.name, c.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
		}
		if c.SuppressesRequestLevelMarker && s.HasAnyRequestLevelCache() {
			t.Fatalf("expected request-level cache markers to be suppressed for %s, contract=%s summary=%+v body=%s", run.target.name, c.Summary(), s, string(run.requestEvent.ProviderRequest.Body))
		}
	}
}
