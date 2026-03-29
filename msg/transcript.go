package msg

import "fmt"

type Transcript []Message

func (t Transcript) PartsByType(partType PartType) Parts {
	filtered := make(Parts, 0)
	for _, m := range t {
		for _, p := range m.Parts {
			if p.Type == partType {
				filtered = append(filtered, p)
			}
		}
	}
	return filtered
}

func (t Transcript) Filter(pred func(Message) bool) Transcript {
	var filtered Transcript
	for _, m := range t {
		if pred(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func (t Transcript) FilterByRole(role Role) Transcript {
	return t.Filter(func(m Message) bool { return m.Role == role })
}

func (t Transcript) System() Transcript { return t.FilterByRole(RoleSystem) }

func BuildTranscript(msg ...IntoMessage) Transcript {
	all := make(Transcript, 0)
	for _, im := range msg {
		m := im.IntoMessage()
		if !m.IsEmpty() {
			all = append(all, m)
		}
	}
	return all
}

func (t Transcript) Validate() error {

	for i, m := range t {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("message #%d: %w", i, err)
		}
	}
	return nil
}
