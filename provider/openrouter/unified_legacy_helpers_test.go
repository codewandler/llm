package openrouter

import (
	"encoding/json"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
)

func buildOpenRouterMessagesBodyLegacy(opts llm.Request) ([]byte, error) {
	strippedModel := opts.Model
	if len(strippedModel) > len("anthropic/") && strippedModel[:len("anthropic/")] == "anthropic/" {
		strippedModel = strippedModel[len("anthropic/"):]
	}
	reqOpts := opts
	reqOpts.Model = strippedModel
	return anthropic.BuildRequestBytes(anthropic.RequestOptions{LLMRequest: reqOpts})
}

func buildOpenRouterResponsesBodyLegacy(opts llm.Request) ([]byte, error) {
	return orRespBuildRequest(opts)
}

func asMap(data []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
