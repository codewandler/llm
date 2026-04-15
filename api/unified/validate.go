package unified

import "fmt"

// Validate validates minimal canonical request invariants.
func (r Request) Validate() error {
	if r.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages are required")
	}
	return nil
}
