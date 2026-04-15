package openai

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/unified"
)

func buildCompletionsBodyUnified(opts llm.Request) ([]byte, error) {
	uReq, err := unified.RequestFromLLM(opts)
	if err != nil {
		return nil, fmt.Errorf("request from llm: %w", err)
	}

	wire, err := unified.RequestToCompletions(uReq)
	if err != nil {
		return nil, fmt.Errorf("request to completions: %w", err)
	}

	return json.Marshal(wire)
}

func buildResponsesBodyUnified(opts llm.Request) ([]byte, error) {
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
