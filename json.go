package llm

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm/tool"
)

type wireToolCalls []tool.Call

func (tc *wireToolCalls) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*tc = make(wireToolCalls, len(raw))
	for i, r := range raw {
		var t struct {
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Args json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(r, &t); err != nil {
			return err
		}
		var args tool.Args
		if len(t.Args) > 0 {
			if err := json.Unmarshal(t.Args, &args); err != nil {
				return err
			}
		}
		(*tc)[i] = tool.NewToolCall(t.ID, t.Name, args)
	}
	return nil
}

type (
	wireTextMsg struct {
		Role      Role       `json:"role"`
		Content   string     `json:"content,omitempty"`
		CacheHint *CacheHint `json:"cache_hint,omitempty"`
	}

	wireAssistantMsg struct {
		wireTextMsg
		ToolCalls wireToolCalls `json:"tool_calls,omitempty"`
	}

	wireToolMsg struct {
		wireTextMsg
		ToolCallID string `json:"tool_call_id"`
		IsError    bool   `json:"is_error,omitempty"`
	}
)

func (m *toolMsg) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireToolMsg{
		wireTextMsg: wireTextMsg{
			Role:    RoleTool,
			Content: m.ToolOutput(),
		},
		ToolCallID: m.ToolCallID(),
		IsError:    m.IsError(),
	})
}

func (m *textMsg) UnmarshalJSON(b []byte) error {

	var w wireTextMsg
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	m.role = w.Role
	m.content = w.Content
	m.cacheHint = w.CacheHint
	return nil
}

func (m *textMsg) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireTextMsg{
		Role:      m.role,
		Content:   m.content,
		CacheHint: m.cacheHint,
	})
}

func (m *assistantMsg) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireAssistantMsg{
		wireTextMsg: wireTextMsg{
			Role:      RoleAssistant,
			Content:   m.content,
			CacheHint: m.cacheHint,
		},
		ToolCalls: m.toolCalls,
	})
}

func (m *Messages) UnmarshalJSON(data []byte) error {
	var rawMessages []json.RawMessage
	if err := json.Unmarshal(data, &rawMessages); err != nil {
		return err
	}

	*m = make(Messages, 0, len(rawMessages))
	for i, raw := range rawMessages {
		// Peek at the role field
		var peek struct {
			Role Role `json:"role"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			return fmt.Errorf("message[%d]: %w", i, err)
		}

		var msg Message
		switch peek.Role {
		case RoleSystem:
			var sm wireTextMsg
			if err := json.Unmarshal(raw, &sm); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = System(sm.Content)
			msg.applyCache(sm.CacheHint)

		case RoleUser:
			var um wireTextMsg
			if err := json.Unmarshal(raw, &um); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = User(um.Content)
			msg.applyCache(um.CacheHint)

		case RoleAssistant:
			var am wireAssistantMsg
			if err := json.Unmarshal(raw, &am); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = Assistant(am.Content, am.ToolCalls...)
			msg.applyCache(am.CacheHint)

		case RoleTool:
			var tr wireToolMsg
			if err := json.Unmarshal(raw, &tr); err != nil {
				return fmt.Errorf("message[%d]: %w", i, err)
			}
			msg = newToolMsg(tr.ToolCallID, tr.Content, tr.IsError)
			msg.applyCache(tr.CacheHint)

		default:
			return fmt.Errorf("message[%d]: unknown role %q", i, peek.Role)
		}

		*m = append(*m, msg)
	}

	return nil
}
