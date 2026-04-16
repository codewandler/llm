package msg

// AssistantPhase preserves OpenAI/OpenRouter Responses assistant output phase
// across request/history roundtrips.
type AssistantPhase string

const (
	AssistantPhaseCommentary  AssistantPhase = "commentary"
	AssistantPhaseFinalAnswer AssistantPhase = "final_answer"
)

func (p AssistantPhase) Valid() bool {
	switch p {
	case "", AssistantPhaseCommentary, AssistantPhaseFinalAnswer:
		return true
	default:
		return false
	}
}

func (p AssistantPhase) IsEmpty() bool { return p == "" }
