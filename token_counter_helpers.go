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
			// map[string]any marshal only fails on unmarshalable types (funcs,
			// channels); tool arguments are always JSON-safe in practice.
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

// anthropicToolPreamble is the number of tokens Anthropic injects as a hidden
// system prompt whenever any tools are present in a request. This preamble
// teaches the model the tool-use protocol and is added once per request,
// regardless of tool count.
//
// Measured empirically via count_tokens API: single-tool baseline ~587 tokens
// vs ~131 raw JSON tokens = ~456 overhead total. Anthropic docs state
// 313–346 tokens for the prompt itself; the remainder is serialisation framing.
//
// Source: https://www.async-let.com/posts/claude-code-mcp-token-reporting/
// and https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview
const anthropicToolPreamble = 330

// anthropicToolFirstOverhead is the serialisation framing added to the first
// tool definition beyond the raw JSON token count (~456 total overhead minus
// the 330 preamble = ~126 for the first tool's wrapper).
const anthropicToolFirstOverhead = 126

// anthropicToolAdditionalOverhead is the framing overhead per additional tool
// beyond the first. Measured: two tools batched = 672 tokens, one tool = 587
// tokens → delta = 85 tokens per additional tool.
const anthropicToolAdditionalOverhead = 85

// CountMessagesAndToolsAnthropic is like CountMessagesAndTools but applies
// Anthropic-specific tool overhead constants: the hidden tool-use system
// preamble (~330 tokens, paid once) plus per-tool serialisation framing
// (~126 tokens first tool, ~85 tokens each additional). In total, a request
// with N tools adds 330+126+(N-1)×85 tokens on top of the raw JSON counts.
//
// Use this for anthropic, bedrock, and claude providers.
func CountMessagesAndToolsAnthropic(tc *TokenCount, req TokenCountRequest) error {
	if err := CountMessagesAndTools(tc, req, tokencount.EncodingCL100K, 0, 0); err != nil {
		return err
	}
	if len(req.Tools) > 0 {
		// Add preamble once + first-tool framing + additional-tool framing.
		toolOverhead := anthropicToolPreamble + anthropicToolFirstOverhead +
			(len(req.Tools)-1)*anthropicToolAdditionalOverhead
		tc.ToolsTokens += toolOverhead
		tc.InputTokens += toolOverhead
	}
	return nil
}

// CountMessagesAndTools is a low-level helper for provider TokenCounter
// implementations. Library consumers should use the TokenCounter interface
// directly rather than calling this function.
//
// It fills tc.PerMessage, tc.ToolsTokens, tc.PerTool, and tc.InputTokens
// using the given BPE encoding, then calls applyRoleBreakdown to populate
// the role breakdown fields.
//
// Returns an error if req.Model is empty.
//
// perMsgOverhead is added to InputTokens once per message (e.g. 4 for the
// OpenAI cookbook formula; 0 for approximation-only providers).
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
