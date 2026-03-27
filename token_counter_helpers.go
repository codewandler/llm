package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm/tokencount"
)

// CountText returns the number of tokens in text for the given model.
// The encoding is selected automatically based on the model ToolCallID:
// o200k_base for GPT-4o/o-series, cl100k_base for everything else.
//
// This is a convenience function for callers that need to count raw text
// without constructing a full TokenCountRequest — for example, context-budget
// managers that count individual history entries.
func CountText(model, text string) (int, error) {
	enc, _ := tokencount.EncodingForModel(model)
	return tokencount.CountText(enc, text)
}

// CountMessage returns the number of tokens for a single Message for the
// given model. The message is converted to its text representation using the
// same logic as CountTokens (role content + tool call names/args for
// AssistantMessage, output for ToolResult, etc.).
//
// This is a convenience function for callers that count messages individually
// rather than as a batch — for example, per-entry token estimates in a
// conversation history manager.
func CountMessage(model string, msg Message) (int, error) {
	enc, _ := tokencount.EncodingForModel(model)
	return tokencount.CountText(enc, messageText(msg))
}

// messageText returns the text content of a message for token counting purposes.
// For AssistantMessage it derives text from ContentBlocks (text blocks only;
// thinking blocks are excluded) plus serialised tool call names/args.
// For ToolResult it uses the output.
func messageText(msg Message) string {
	switch m := msg.(type) {
	case AssistantMessage:
		// Derive text from content blocks (text blocks only; thinking blocks excluded).
		var sb strings.Builder
		for _, b := range m.ContentBlocks() {
			if b.Kind == ContentBlockKindText {
				sb.WriteString(b.Text)
			}
		}
		text := sb.String()
		for _, tc := range m.ToolCalls() {
			// map[string]any marshal only fails on unmarshalable types (funcs,
			// channels); tool arguments are always JSON-safe in practice.
			args, _ := json.Marshal(tc.ToolArgs())
			text += " " + tc.ToolName() + " " + string(args)
		}
		return text
	case ToolMessage:
		return m.ToolOutput()
	case TextMessage:
		return m.Content()
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

// CountOpts configures the shared CountMessagesAndTools helper.
type CountOpts struct {
	// Encoding is the BPE encoding to use for token counting
	// (e.g. "cl100k_base", "o200k_base", "minimax_bpe").
	Encoding string

	// PerMsgOverhead is added to InputTokens once per message. For example,
	// OpenAI adds 4 tokens per message for role/framing overhead.
	PerMsgOverhead int

	// ReplyPriming is a fixed addend for reply-priming tokens. For example,
	// OpenAI adds 3 tokens for the "assistant" token prepended by the API.
	ReplyPriming int
}

// CountMessagesAndToolsAnthropic is like CountMessagesAndTools but applies
// Anthropic-specific tool overhead constants: the hidden tool-use system
// preamble (~330 tokens, paid once) plus per-tool serialisation framing
// (~126 tokens first tool, ~85 tokens each additional). In total, a request
// with N tools adds 330+126+(N-1)×85 tokens on top of the raw JSON counts.
//
// Use this for anthropic, bedrock, and claude providers.
func CountMessagesAndToolsAnthropic(tc *TokenCount, req TokenCountRequest) error {
	if err := CountMessagesAndTools(tc, req, CountOpts{Encoding: tokencount.EncodingCL100K}); err != nil {
		return err
	}
	if len(req.Tools) > 0 {
		ApplyAnthropicToolOverhead(tc, len(req.Tools))
	}
	return nil
}

// ApplyAnthropicToolOverhead adds the Anthropic tool-use preamble and per-tool
// serialisation framing to tc.OverheadTokens and tc.InputTokens.
//
// This is exported so that providers using the Anthropic API format (e.g. MiniMax)
// can apply the same overhead after calling CountMessagesAndTools with their own
// encoding.
func ApplyAnthropicToolOverhead(tc *TokenCount, numTools int) {
	if numTools <= 0 {
		return
	}
	// Track the preamble + framing as provider overhead, separate from the
	// raw tool JSON counts in ToolsTokens. This keeps sum(PerTool)==ToolsTokens.
	toolOverhead := anthropicToolPreamble + anthropicToolFirstOverhead +
		(numTools-1)*anthropicToolAdditionalOverhead
	tc.OverheadTokens += toolOverhead
	tc.InputTokens += toolOverhead
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
func CountMessagesAndTools(tc *TokenCount, req TokenCountRequest, opts CountOpts) error {
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
		n, err := tokencount.CountText(opts.Encoding, text)
		if err != nil {
			return fmt.Errorf("llm: count tokens for message[%d]: %w", i, err)
		}
		tc.PerMessage[i] = n
		total += n + opts.PerMsgOverhead
	}

	// Count each tool definition
	toolTotal := 0
	for _, tool := range tools {
		b, err := json.Marshal(tool)
		if err != nil {
			return fmt.Errorf("llm: marshal tool %q: %w", tool.Name, err)
		}
		n, err := tokencount.CountText(opts.Encoding, string(b))
		if err != nil {
			return fmt.Errorf("llm: count tokens for tool %q: %w", tool.Name, err)
		}
		tc.PerTool[tool.Name] = n
		toolTotal += n
	}
	tc.ToolsTokens = toolTotal
	total += toolTotal + opts.ReplyPriming

	tc.InputTokens = total
	applyRoleBreakdown(tc, msgs)
	return nil
}
