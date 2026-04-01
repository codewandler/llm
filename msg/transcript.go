package msg

import "fmt"

type Messages []Message

func (t Messages) Append(m IntoMessages) Messages {
	return append(t, m.IntoMessages()...)
}

func (t Messages) IntoMessages() []Message { return t }

func (t Messages) PartsByType(partType PartType) Parts {
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

func (t Messages) Filter(pred func(Message) bool) Messages {
	var filtered Messages
	for _, m := range t {
		if pred(m) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func (t Messages) FilterByRole(role Role) Messages {
	return t.Filter(func(m Message) bool { return m.Role == role })
}

func (t Messages) System() Messages { return t.FilterByRole(RoleSystem) }

func BuildTranscript(msg ...IntoMessages) Messages {
	all := make(Messages, 0)
	for _, im := range msg {
		if im == nil {
			continue
		}
		mm := im.IntoMessages()
		for _, m := range mm {
			if !m.IsEmpty() {
				all = append(all, m)
			}
		}

	}
	return all
}

func (t Messages) Validate() error {

	for i, m := range t {
		if err := m.Validate(); err != nil {
			return fmt.Errorf("message #%d: %w", i, err)
		}
	}
	return nil
}
