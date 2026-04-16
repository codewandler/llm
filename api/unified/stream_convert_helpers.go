package unified

import (
	"encoding/json"

	"github.com/codewandler/llm/api/apicore"
)

func uint32Ptr(v int) *uint32 {
	u := uint32(v)
	return &u
}

func withRawEventName(ev StreamEvent, name string) StreamEvent {
	ev.Extras.RawEventName = name
	return ev
}

func withProviderExtras(ev StreamEvent, provider any) StreamEvent {
	m := providerMap(provider)
	if len(m) > 0 {
		ev.Extras.Provider = m
	}
	return ev
}

func withRawEventPayload(ev StreamEvent, payload any) StreamEvent {
	_, rawName, rawJSON := sourceEvent(payload)
	if ev.Extras.RawEventName == "" {
		ev.Extras.RawEventName = rawName
	}
	if len(ev.Extras.RawJSON) == 0 && len(rawJSON) > 0 {
		ev.Extras.RawJSON = append([]byte(nil), rawJSON...)
	}
	return ev
}

func sourceEvent(v any) (payload any, rawName string, rawJSON []byte) {
	switch x := v.(type) {
	case apicore.StreamResult:
		return x.Event, x.RawEventName, x.RawJSON
	case *apicore.StreamResult:
		if x == nil {
			return nil, "", nil
		}
		return x.Event, x.RawEventName, x.RawJSON
	default:
		return v, "", nil
	}
}

func providerMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
