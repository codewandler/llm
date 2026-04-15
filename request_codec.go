package llm

import "fmt"

// --- ThinkingMode codec ---

// MarshalText maps the zero value to the user-visible string "auto".
func (m ThinkingMode) MarshalText() ([]byte, error) {
	if m == ThinkingAuto { // ThinkingAuto = ""
		return []byte("auto"), nil
	}
	return []byte(m), nil
}

// UnmarshalText maps "auto" → ThinkingAuto (the zero value "").
// An empty string is also accepted: ThinkingAuto == "" passes Valid().
func (m *ThinkingMode) UnmarshalText(b []byte) error {
	s := string(b)
	if s == "auto" {
		*m = ThinkingAuto
		return nil
	}
	v := ThinkingMode(s)
	if !v.Valid() {
		return fmt.Errorf("invalid thinking mode %q: must be auto, on, or off", s)
	}
	*m = v
	return nil
}

// --- Effort codec ---

func (e Effort) MarshalText() ([]byte, error) { return []byte(e), nil }

func (e *Effort) UnmarshalText(b []byte) error {
	v := Effort(b)
	if !v.Valid() {
		return fmt.Errorf("invalid effort %q: must be low, medium, high, or max", v)
	}
	*e = v
	return nil
}

// --- OutputFormat codec ---

func (f OutputFormat) MarshalText() ([]byte, error) { return []byte(f), nil }

func (f *OutputFormat) UnmarshalText(b []byte) error {
	v := OutputFormat(b)
	switch v {
	case "", OutputFormatText, OutputFormatJSON:
		*f = v
		return nil
	}
	return fmt.Errorf("invalid output-format %q: must be text or json", v)
}

// --- ApiType codec ---

// MarshalText maps the zero value (ApiTypeAuto = "") to the user-visible string "auto",
// matching the ThinkingMode convention.
func (t ApiType) MarshalText() ([]byte, error) {
	if t == ApiTypeAuto {
		return []byte("auto"), nil
	}
	return []byte(t), nil
}

// UnmarshalText maps "auto" → ApiTypeAuto (the zero value "").
// An empty input is also accepted as ApiTypeAuto.
func (t *ApiType) UnmarshalText(b []byte) error {
	s := string(b)
	if s == "auto" || s == "" {
		*t = ApiTypeAuto
		return nil
	}
	v := ApiType(s)
	if !v.Valid() {
		return fmt.Errorf("invalid api type %q; must be one of: auto, openai-chat, openai-responses, anthropic-messages", s)
	}
	*t = v
	return nil
}
