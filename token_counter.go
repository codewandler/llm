package llm

import "context"

// TokenCountRequest is the input to TokenCounter.CountTokens.
// Model is required — providers use it to select the correct BPE encoding.
type TokenCountRequest struct {
	// Model is the model ID to count tokens for (e.g. "gpt-4o", "claude-sonnet-4-5").
	// Required — returns an error if empty.
	Model    string
	Messages Messages
	Tools    []ToolDefinition
}

// TokenCounter is an optional interface providers may implement to estimate
// token usage before sending a request.
//
// All implementations in this codebase are local/offline — no network call is
// made. Counts should be treated as estimates; accuracy varies by provider:
//   - OpenAI: exact (tiktoken matches the API tokenizer)
//   - OpenRouter: approximate (tiktoken, best-effort model prefix matching)
//   - Anthropic: approximate (cl100k_base, ±5-10% for English; tokenizer not public)
//   - Bedrock: approximate (same as Anthropic)
//   - Ollama: approximate (cl100k_base; no public tokenize endpoint)
//
// Usage:
//
//	if tc, ok := provider.(llm.TokenCounter); ok {
//	    count, err := tc.CountTokens(ctx, llm.TokenCountRequest{
//	        Model:    "gpt-4o",
//	        Messages: messages,
//	        Tools:    tools,
//	    })
//	    if err == nil && count.InputTokens > maxTokens {
//	        return fmt.Errorf("request too large: %d tokens (limit %d)", count.InputTokens, maxTokens)
//	    }
//	}
type TokenCounter interface {
	CountTokens(ctx context.Context, req TokenCountRequest) (*TokenCount, error)
}

// TokenCount holds the result of a CountTokens call.
//
// Invariants:
//   - len(PerMessage) == len(StreamRequest.Messages)
//   - SystemTokens + UserTokens + AssistantTokens + ToolResultTokens == sum(PerMessage)
//   - sum(values(PerTool)) == ToolsTokens
//   - InputTokens == sum(PerMessage) + ToolsTokens + provider-specific overhead
type TokenCount struct {
	// InputTokens is the total estimated input token count:
	// all messages + all tool definitions + any provider-specific overhead.
	InputTokens int

	// PerMessage contains the token count for each entry in StreamRequest.Messages,
	// in the same index order. Does not include tool definitions or overhead.
	// len(PerMessage) == len(StreamRequest.Messages) is guaranteed.
	PerMessage []int

	// Role breakdowns — derived from PerMessage, provided for convenience.
	// SystemTokens + UserTokens + AssistantTokens + ToolResultTokens == sum(PerMessage).
	SystemTokens     int // sum of PerMessage for all RoleSystem messages
	UserTokens       int // sum of PerMessage for all RoleUser messages
	AssistantTokens  int // sum of PerMessage for all RoleAssistant messages
	ToolResultTokens int // sum of PerMessage for all RoleTool (ToolCallResult) messages

	// ToolsTokens is the total estimated tokens for all tool definitions combined.
	ToolsTokens int

	// PerTool maps each tool definition's Name to its individual token count.
	// sum(values(PerTool)) == ToolsTokens.
	PerTool map[string]int
}

// applyRoleBreakdown fills tc.SystemTokens, tc.UserTokens, tc.AssistantTokens,
// and tc.ToolResultTokens by walking msgs and tc.PerMessage together.
//
// It must be called after PerMessage is fully populated. It is provided as a
// shared helper so that provider implementations do not duplicate this logic.
//
// Panics if len(tc.PerMessage) != len(msgs) — callers must ensure the invariant.
func applyRoleBreakdown(tc *TokenCount, msgs Messages) {
	if len(tc.PerMessage) != len(msgs) {
		panic("llm: applyRoleBreakdown: len(PerMessage) != len(msgs)")
	}
	for i, msg := range msgs {
		n := tc.PerMessage[i]
		switch msg.Role() {
		case RoleSystem:
			tc.SystemTokens += n
		case RoleUser:
			tc.UserTokens += n
		case RoleAssistant:
			tc.AssistantTokens += n
		case RoleTool:
			tc.ToolResultTokens += n
		}
	}
}
