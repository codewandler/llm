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

// ModelAliases maps short alias names to full model IDs.
// Used by the auto package for provider-prefixed resolution (e.g., "minimax/fast").
var ModelAliases = map[string]string{
	"minimax":      ModelM27,
	"minimax:fast": ModelM27,
	"minimax:2.7":  ModelM27,
	"minimax:2.5":  ModelM25,
	"minimax:2.1":  ModelM21,
	"minimax:2":    ModelM2,
}

