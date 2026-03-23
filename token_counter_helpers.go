package llm

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm/tokencount"
)

// messageText returns the text content of a message for token counting purposes.
// For AssistantMsg it concatenates text content and serialised tool call names/args.
// For ToolCallResult it uses the output.
func messageText(msg Message) string {
	switch m := msg.(type) {
	case *SystemMsg:
		return m.Content
	case *UserMsg:
		return m.Content
	case *AssistantMsg:
		text := m.Content
		for _, tc := range m.ToolCalls {
			// Count tool name + JSON arguments
			args, _ := json.Marshal(tc.Arguments)
			text += " " + tc.Name + " " + string(args)
		}
		return text
	case *ToolCallResult:
		return m.Output
	default:
		// Fallback: marshal and count the raw JSON
		b, _ := json.Marshal(msg)
		return string(b)
	}
}

// CountMessagesAndTools is a shared helper used by provider TokenCounter
// implementations. It fills tc.PerMessage, tc.ToolsTokens, tc.PerTool, and
// tc.InputTokens using the given BPE encoding, then calls applyRoleBreakdown
// to populate the role breakdown fields.
//
// Returns an error if req.Model is empty.
//
// perMsgOverhead is added to InputTokens once per message (e.g. 4 for OpenAI
// cookbook formula; 0 for approximation-only providers).
// replyPriming is a fixed addend for reply-priming tokens (e.g. 3 for OpenAI;
// 0 for others).
func CountMessagesAndTools(tc *TokenCount, req TokenCountRequest, encoding string, perMsgOverhead int, replyPriming int) error {
	if req.Model == "" {
		return fmt.Errorf("llm: CountTokens: model is required")
	}

	msgs := req.Messages
	tools := req.Tools
	tc.PerMessage = make([]int, len(msgs))
	tc.PerTool = make(map[string]int, len(tools))

	total := 0

	// Count each message
	for i, msg := range msgs {
		text := messageText(msg)
		n, err := tokencount.CountText(encoding, text)
		if err != nil {
			return fmt.Errorf("llm: count tokens for message[%d]: %w", i, err)
		}
		tc.PerMessage[i] = n
		total += n + perMsgOverhead
	}

	// Count each tool definition
	toolTotal := 0
	for _, tool := range tools {
		b, err := json.Marshal(tool)
		if err != nil {
			return fmt.Errorf("llm: marshal tool %q: %w", tool.Name, err)
		}
		n, err := tokencount.CountText(encoding, string(b))
		if err != nil {
			return fmt.Errorf("llm: count tokens for tool %q: %w", tool.Name, err)
		}
		tc.PerTool[tool.Name] = n
		toolTotal += n
	}
	tc.ToolsTokens = toolTotal
	total += toolTotal + replyPriming

	tc.InputTokens = total
	applyRoleBreakdown(tc, msgs)
	return nil
}
