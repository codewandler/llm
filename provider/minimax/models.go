package minimax

// Model ToolCallID constants for programmatic use.
const (
	ModelM27          = "MiniMax-M2.7"
	ModelM27Highspeed = "MiniMax-M2.7-highspeed"
	ModelM25          = "MiniMax-M2.5"
	ModelM25Highspeed = "MiniMax-M2.5-highspeed"
	ModelM21          = "MiniMax-M2.1"
	ModelM21Highspeed = "MiniMax-M2.1-highspeed"
	ModelM2           = "MiniMax-M2"
)

// ModelAliases maps provider-scoped shorthand aliases to full model IDs.
// These are provider policy aliases used for resolution like "minimax/fast";
// they are not the canonical source of truth for model discovery.
var ModelAliases = map[string]string{
	"minimax":      ModelM27,
	"minimax:fast": ModelM27,
	"minimax:2.7":  ModelM27,
	"minimax:2.5":  ModelM25,
	"minimax:2.1":  ModelM21,
	"minimax:2":    ModelM2,
}
