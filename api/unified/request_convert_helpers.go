package unified

import (
	"encoding/json"
	"strings"
)

func partsText(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == PartTypeText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func toMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func contentString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
