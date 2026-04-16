package unified

import (
	"fmt"

	"github.com/codewandler/llm/msg"
)

// Validate validates canonical unified request invariants.
func (r Request) Validate() error {
	if r.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("messages are required")
	}
	if err := validateOutput(r.Output); err != nil {
		return err
	}
	if err := validateCacheHint("request cache hint", r.CacheHint); err != nil {
		return err
	}
	for i, m := range r.Messages {
		if err := validateCacheHint(fmt.Sprintf("messages[%d].cache_hint", i), m.CacheHint); err != nil {
			return err
		}
	}
	return nil
}

func validateOutput(out *OutputSpec) error {
	if out == nil {
		return nil
	}
	switch out.Mode {
	case OutputModeText, OutputModeJSONObject:
		if out.Schema != nil {
			return fmt.Errorf("output schema requires mode json_schema")
		}
		return nil
	case OutputModeJSONSchema:
		if out.Schema == nil {
			return fmt.Errorf("output json_schema mode requires schema")
		}
		return nil
	default:
		return fmt.Errorf("invalid output mode %q", out.Mode)
	}
}

func validateCacheHint(field string, h *msg.CacheHint) error {
	if h == nil {
		return nil
	}
	switch h.TTL {
	case "", msg.CacheTTL5m.String(), msg.CacheTTL1h.String():
		return nil
	default:
		return fmt.Errorf("%s ttl must be one of: %q, %q, %q", field, "", msg.CacheTTL5m.String(), msg.CacheTTL1h.String())
	}
}
