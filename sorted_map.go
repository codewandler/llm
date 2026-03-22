package llm

import (
	"encoding/json"
	"sort"
)

// SortedMap is a map[string]any that serialises its keys in alphabetical order.
// This guarantees deterministic JSON output for tool schema definitions, which
// is required for stable prompt-cache fingerprints on providers that hash tool
// definitions (Anthropic, Bedrock).
//
// Construct with NewSortedMap. The zero value is valid and marshals as {}.
type SortedMap struct {
	keys   []string
	values []any
}

// NewSortedMap converts a map[string]any into a SortedMap whose keys are sorted
// alphabetically at every level of nesting. Nested map[string]any values and
// []any arrays are recursed so that all object nodes in the tree are also sorted.
// A nil or empty map produces a SortedMap that marshals as {}.
func NewSortedMap(m map[string]any) *SortedMap {
	sm := &SortedMap{
		keys:   make([]string, 0, len(m)),
		values: make([]any, 0, len(m)),
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		sm.keys = append(sm.keys, k)
		sm.values = append(sm.values, sortedMapValue(m[k]))
	}
	return sm
}

// sortedMapValue recursively converts any map[string]any or []any node in the
// schema value tree into its sorted equivalent.
func sortedMapValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return NewSortedMap(val)
	case []any:
		out := make([]any, len(val))
		for i, elem := range val {
			out[i] = sortedMapValue(elem)
		}
		return out
	default:
		return v
	}
}

// MarshalJSON implements json.Marshaler. Keys are emitted in the order they
// were inserted (alphabetical, because NewSortedMap sorts them on construction).
func (sm *SortedMap) MarshalJSON() ([]byte, error) {
	// Estimate capacity: 2 bytes per brace + per-entry overhead.
	buf := make([]byte, 0, 2+len(sm.keys)*16)
	buf = append(buf, '{')

	for i, k := range sm.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		// Marshal the key as a JSON string.
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')

		// Marshal the value; nested SortedMaps recurse via their own MarshalJSON.
		vb, err := json.Marshal(sm.values[i])
		if err != nil {
			return nil, err
		}
		buf = append(buf, vb...)
	}

	buf = append(buf, '}')
	return buf, nil
}
