package openrouter

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/unified"
)

func buildOpenRouterMessagesBodyUnified(opts llm.Request) ([]byte, error) {
	strippedModel := opts.Model
	if len(strippedModel) > len("anthropic/") && strippedModel[:len("anthropic/")] == "anthropic/" {
		strippedModel = strippedModel[len("anthropic/"):]
	}
	reqOpts := opts
	reqOpts.Model = strippedModel

	uReq, err := unified.RequestFromLLM(reqOpts)
	if err != nil {
		return nil, fmt.Errorf("request from llm: %w", err)
	}
	wire, err := unified.RequestToMessages(uReq)
	if err != nil {
		return nil, fmt.Errorf("request to messages: %w", err)
	}
	return json.Marshal(wire)
}

func buildOpenRouterResponsesBodyUnified(opts llm.Request) ([]byte, error) {
	uReq, err := unified.RequestFromLLM(opts)
	if err != nil {
		return nil, fmt.Errorf("request from llm: %w", err)
	}
	wire, err := unified.RequestToResponses(uReq)
	if err != nil {
		return nil, fmt.Errorf("request to responses: %w", err)
	}
	return json.Marshal(wire)
}
