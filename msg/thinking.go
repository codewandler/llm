package msg

import "errors"

type ThinkingPart struct {
	Provider  string `json:"provider,omitempty"`
	Text      string `json:"text,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func (p ThinkingPart) IntoPart() Part       { return Part{Type: PartTypeThinking, Thinking: &p} }
func (p ThinkingPart) IntoMessage() Message { return Assistant(p).Build() }

func (p ThinkingPart) Validate() error {
	if p.Text == "" {
		return errors.New("thinking: text is required")
	}
	return nil
}
