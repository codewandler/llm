package llm

import "github.com/codewandler/llm/msg"

type (
	Role      = msg.Role
	Message   = msg.Message
	Messages  = msg.Messages
	CacheHint = msg.CacheHint

	// CacheOpt and CacheTTL are re-exported from the msg package so callers
	// using RequestBuilder do not need to import msg directly.
	CacheOpt = msg.CacheOpt
	CacheTTL = msg.CacheTTL
)

const (
	RoleSystem    = msg.RoleSystem
	RoleUser      = msg.RoleUser
	RoleAssistant = msg.RoleAssistant
	RoleTool      = msg.RoleTool
	RoleDeveloper = msg.RoleDeveloper

	// Cache TTL convenience aliases.
	CacheTTL5m = msg.CacheTTL5m
	CacheTTL1h = msg.CacheTTL1h
)

func System(text string) Message    { return msg.System(text).Build() }
func User(text string) Message      { return msg.User(text).Build() }
func Assistant(text string) Message { return msg.Assistant(msg.Text(text)).Build() }
