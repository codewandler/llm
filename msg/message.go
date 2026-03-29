package msg

import "fmt"

type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role      Role       `json:"role"`
	Parts     Parts      `json:"parts"`
	CacheHint *CacheHint `json:"cache_hint,omitempty"`
}

func (m Message) Text() string             { return m.Parts.Text() }
func (m Message) IsSystem() bool           { return m.Role == RoleSystem }
func (m Message) IsUser() bool             { return m.Role == RoleUser }
func (m Message) IsAssistant() bool        { return m.Role == RoleAssistant }
func (m Message) IsTool() bool             { return m.Role == RoleTool }
func (m Message) IsDeveloper() bool        { return m.Role == RoleDeveloper }
func (m Message) IsEmptyText() bool        { return m.Text() == "" }
func (m Message) ToolCalls() ToolCalls     { return m.Parts.ToolCalls() }
func (m Message) ToolResults() ToolResults { return m.Parts.ToolResults() }
func (m Message) IsEmpty() bool {
	if m.Role == "" && len(m.Parts) == 0 {
		return true
	}
	return false

}
func (m Message) Validate() error {
	if m.Role == "" {
		return fmt.Errorf("message: role is required")
	}

	if len(m.Parts) == 0 {
		return fmt.Errorf("message: parts is required")
	}

	// Validate content-specific rules
	switch m.Role {
	case RoleSystem, RoleUser:
		if m.Text() == "" {
			return fmt.Errorf("message: text content is required for %s role", m.Role)
		}
	case RoleTool:
		results := m.ToolResults()
		if len(results) == 0 {
			return fmt.Errorf("message: tool result is required for tool role")
		}
		for _, r := range results {
			if r.ToolCallID == "" {
				return fmt.Errorf("message: tool_call_id is required")
			}
			if r.ToolOutput == "" {
				return fmt.Errorf("message: output is required")
			}
		}
	}

	return nil
}

type (
	IntoMessage  interface{ IntoMessage() Message }
	IntoMessages interface{ IntoMessages() []Message }
)

func (m Message) IntoMessage() Message    { return m }
func (m Message) IntoMessages() []Message { return []Message{m} }
