package unified

import "encoding/json"

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
	b, err := json.Marshal(payload)
	if err == nil {
		ev.Extras.RawJSON = b
	}
	return ev
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
