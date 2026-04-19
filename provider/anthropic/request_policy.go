package anthropic

import providercore2 "github.com/codewandler/llm/internal/providercore"

func CoerceAnthropicThinkingTemperature(msgReq *providercore2.MessagesRequest) {
	if msgReq == nil || msgReq.Thinking == nil || msgReq.Thinking.Type == "disabled" {
		return
	}
	if msgReq.Temperature != 0 && msgReq.Temperature != 1 {
		msgReq.Temperature = 1
	}
}
