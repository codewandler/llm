package llm

import "github.com/codewandler/llm/msg"

type (
	Role      = msg.Role
	Message   = msg.Message
	Messages  = msg.Messages
	CacheHint = msg.CacheHint
)

const (
	RoleSystem    = msg.RoleSystem
	RoleUser      = msg.RoleUser
	RoleAssistant = msg.RoleAssistant
	RoleTool      = msg.RoleTool
	RoleDeveloper = msg.RoleDeveloper
)

func System(text string) Message    { return msg.System(text).Build() }
func User(text string) Message      { return msg.User(text).Build() }
func Assistant(text string) Message { return msg.Assistant(msg.Text(text)).Build() }
