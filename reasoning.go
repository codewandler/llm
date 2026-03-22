package llm

// --- ReasoningEffort ---

// ReasoningEffort controls the amount of reasoning for reasoning models.
// Lower values result in faster responses with fewer reasoning tokens.
type ReasoningEffort string

const (
	// ReasoningEffortNone disables reasoning (GPT-5.1+ only).
	ReasoningEffortNone ReasoningEffort = "none"
	// ReasoningEffortMinimal uses minimal reasoning effort.
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	// ReasoningEffortLow uses low reasoning effort.
	ReasoningEffortLow ReasoningEffort = "low"
	// ReasoningEffortMedium uses medium reasoning effort (default for most models before GPT-5.1).
	ReasoningEffortMedium ReasoningEffort = "medium"
	// ReasoningEffortHigh uses high reasoning effort.
	ReasoningEffortHigh ReasoningEffort = "high"
	// ReasoningEffortXHigh uses extra high reasoning effort (codex-max+ only).
	ReasoningEffortXHigh ReasoningEffort = "xhigh"
)

// Valid returns true if the ReasoningEffort is a known valid value or empty.
func (r ReasoningEffort) Valid() bool {
	switch r {
	case "", ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow,
		ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh:
		return true
	default:
		return false
	}
}
