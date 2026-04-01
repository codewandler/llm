package claude

import "github.com/codewandler/llm"

func normalizeRequest(req *llm.Request) {
	if req.Messages == nil {
		req.Messages = make([]llm.Message, 0)
	}

	if req.Model == "" {
		req.Model = ModelDefault
	}
}
