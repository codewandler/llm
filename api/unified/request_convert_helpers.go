package unified

import (
	"encoding/json"
	"strings"

	"github.com/codewandler/llm/msg"
)

func partsText(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == PartTypeText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func toMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func contentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func promptCacheRetentionFromHint(h *msg.CacheHint) string {
	if h != nil && h.Enabled && h.TTL == msg.CacheTTL1h.String() {
		return "24h"
	}
	return ""
}

func cacheHintFromPromptCacheRetention(ret string) *msg.CacheHint {
	if ret == "24h" {
		return &msg.CacheHint{Enabled: true, TTL: msg.CacheTTL1h.String()}
	}
	return nil
}

func outputFromLLM(format string) *OutputSpec {
	switch format {
	case "json":
		return &OutputSpec{Mode: OutputModeJSONObject}
	case "text":
		return &OutputSpec{Mode: OutputModeText}
	default:
		return nil
	}
}

func metadataToOpenAI(meta *RequestMetadata, extra map[string]any) (string, map[string]any) {
	user := ""
	out := cloneAnyMap(extra)
	if meta != nil {
		user = meta.User
		if len(meta.Metadata) > 0 {
			if out == nil {
				out = map[string]any{}
			}
			for k, v := range meta.Metadata {
				out[k] = v
			}
		}
	}
	if len(out) == 0 {
		out = nil
	}
	return user, out
}

func metadataFromOpenAI(user string, raw map[string]any) (*RequestMetadata, map[string]any) {
	if user == "" && len(raw) == 0 {
		return nil, nil
	}
	return &RequestMetadata{User: user, Metadata: cloneAnyMap(raw)}, nil
}

func ensureMessagesExtras(r *Request) *MessagesExtras {
	if r.Extras.Messages == nil {
		r.Extras.Messages = &MessagesExtras{}
	}
	return r.Extras.Messages
}

func messagesCachePartIndex(r Request, messageIndex int) *int {
	if r.Extras.Messages == nil || r.Extras.Messages.MessageCachePartIndex == nil {
		return nil
	}
	idx, ok := r.Extras.Messages.MessageCachePartIndex[messageIndex]
	if !ok {
		return nil
	}
	return &idx
}

func ensureCompletionsExtras(r *Request) *CompletionsExtras {
	if r.Extras.Completions == nil {
		r.Extras.Completions = &CompletionsExtras{}
	}
	return r.Extras.Completions
}

func ensureResponsesExtras(r *Request) *ResponsesExtras {
	if r.Extras.Responses == nil {
		r.Extras.Responses = &ResponsesExtras{}
	}
	return r.Extras.Responses
}
