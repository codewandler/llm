package providercore

import (
	agentunified "github.com/codewandler/agentapis/api/unified"
	"github.com/codewandler/llm"
)

func RequestToUnified(req llm.Request) (agentunified.Request, error) {
	return requestToAgentUnified(req)
}
