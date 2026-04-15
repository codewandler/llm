package unified

import (
	"fmt"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	llmtool "github.com/codewandler/llm/tool"
)

// RequestFromLLM converts a public llm.Request to the canonical unified schema.
func RequestFromLLM(req llm.Request) (Request, error) {
	if err := req.Validate(); err != nil {
		return Request{}, fmt.Errorf("validate llm request: %w", err)
	}

	u := Request{
		Model:        req.Model,
		MaxTokens:    req.MaxTokens,
		Temperature:  req.Temperature,
		TopP:         req.TopP,
		TopK:         req.TopK,
		OutputFormat: req.OutputFormat,
		Effort:       req.Effort,
		Thinking:     req.Thinking,
		CacheHint:    req.CacheHint,
		ApiTypeHint:  req.ApiTypeHint,
		ToolChoice:   req.ToolChoice,
	}

	for _, t := range req.Tools {
		u.Tools = append(u.Tools, Tool{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}

	u.Messages = make([]Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		u.Messages = append(u.Messages, messageFromLLM(m))
	}

	return u, nil
}

// RequestToLLM converts a canonical unified request back to llm.Request.
// This is best-effort and intended mainly for tooling/debugging/roundtrip tests.
func RequestToLLM(req Request) (llm.Request, error) {
	if err := req.Validate(); err != nil {
		return llm.Request{}, fmt.Errorf("validate unified request: %w", err)
	}

	out := llm.Request{
		Model:        req.Model,
		MaxTokens:    req.MaxTokens,
		Temperature:  req.Temperature,
		TopP:         req.TopP,
		TopK:         req.TopK,
		OutputFormat: req.OutputFormat,
		Effort:       req.Effort,
		Thinking:     req.Thinking,
		CacheHint:    req.CacheHint,
		ApiTypeHint:  req.ApiTypeHint,
		ToolChoice:   req.ToolChoice,
	}

	for _, t := range req.Tools {
		out.Tools = append(out.Tools, llmtool.Definition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}

	out.Messages = make(llm.Messages, 0, len(req.Messages))
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, messageToLLM(m))
	}

	return out, nil
}

func messageFromLLM(m msg.Message) Message {
	um := Message{
		Role:      Role(m.Role),
		CacheHint: m.CacheHint,
		Parts:     make([]Part, 0, len(m.Parts)),
	}
	for _, p := range m.Parts {
		um.Parts = append(um.Parts, partFromLLM(p))
	}
	return um
}

func partFromLLM(p msg.Part) Part {
	up := Part{Type: PartType(p.Type)}
	switch p.Type {
	case msg.PartTypeText:
		up.Text = p.Text
	case msg.PartTypeThinking:
		if p.Thinking != nil {
			up.Thinking = &ThinkingPart{
				Provider:  p.Thinking.Provider,
				Text:      p.Thinking.Text,
				Signature: p.Thinking.Signature,
			}
		}
	case msg.PartTypeToolCall:
		if p.ToolCall != nil {
			up.ToolCall = &ToolCall{ID: p.ToolCall.ID, Name: p.ToolCall.Name, Args: p.ToolCall.Args}
		}
	case msg.PartTypeToolResult:
		if p.ToolResult != nil {
			up.ToolResult = &ToolResult{
				ToolCallID: p.ToolResult.ToolCallID,
				ToolOutput: p.ToolResult.ToolOutput,
				IsError:    p.ToolResult.IsError,
			}
		}
	}
	return up
}

func messageToLLM(m Message) msg.Message {
	out := msg.Message{Role: msg.Role(m.Role), CacheHint: m.CacheHint}
	out.Parts = make(msg.Parts, 0, len(m.Parts))
	for _, p := range m.Parts {
		out.Parts = append(out.Parts, partToLLM(p))
	}
	return out
}

func partToLLM(p Part) msg.Part {
	out := msg.Part{Type: msg.PartType(p.Type)}
	switch p.Type {
	case PartTypeText:
		out.Text = p.Text
	case PartTypeThinking:
		if p.Thinking != nil {
			out.Thinking = &msg.ThinkingPart{
				Provider:  p.Thinking.Provider,
				Text:      p.Thinking.Text,
				Signature: p.Thinking.Signature,
			}
		}
	case PartTypeToolCall:
		if p.ToolCall != nil {
			out.ToolCall = &msg.ToolCall{ID: p.ToolCall.ID, Name: p.ToolCall.Name, Args: p.ToolCall.Args}
		}
	case PartTypeToolResult:
		if p.ToolResult != nil {
			out.ToolResult = &msg.ToolResult{
				ToolCallID: p.ToolResult.ToolCallID,
				ToolOutput: p.ToolResult.ToolOutput,
				IsError:    p.ToolResult.IsError,
			}
		}
	}
	return out
}
