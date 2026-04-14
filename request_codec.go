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
