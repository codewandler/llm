package catalog

import "strings"

// ModelKey is the canonical identity of a model within the catalog.
type ModelKey struct {
	Creator     string `json:"creator"`
	Family      string `json:"family"`
	Series      string `json:"series,omitempty"`
	Version     string `json:"version,omitempty"`
	Variant     string `json:"variant,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"`
}

// NormalizeKey canonicalizes case and date formatting.
func NormalizeKey(key ModelKey) ModelKey {
	key.Creator = normalizeKeyPart(key.Creator)
	key.Family = normalizeKeyPart(key.Family)
	key.Series = normalizeKeyPart(key.Series)
	key.Version = normalizeKeyPart(key.Version)
	key.Variant = normalizeKeyPart(key.Variant)
	key.ReleaseDate = normalizeDate(key.ReleaseDate)
	return key
}

// LineKey drops release-specific information from the key.
func LineKey(key ModelKey) ModelKey {
	key = NormalizeKey(key)
	key.ReleaseDate = ""
	return key
}

// IsRelease reports whether the key describes a dated release.
func IsRelease(key ModelKey) bool {
	return NormalizeKey(key).ReleaseDate != ""
}

// LineID renders a stable line-level string ID for the key.
func LineID(key ModelKey) string {
	key = LineKey(key)
	parts := make([]string, 0, 5)
	for _, part := range []string{key.Creator, key.Family, key.Series, key.Version, key.Variant} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// ReleaseID renders a stable release-specific string ID for the key.
func ReleaseID(key ModelKey) string {
	key = NormalizeKey(key)
	if key.ReleaseDate == "" {
		return ""
	}
	return LineID(key) + "@" + key.ReleaseDate
}

func normalizeKeyPart(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.ReplaceAll(v, "_", "-")
	v = strings.ReplaceAll(v, " ", "-")
	for strings.Contains(v, "--") {
		v = strings.ReplaceAll(v, "--", "-")
	}
	return v
}

func normalizeDate(v string) string {
	v = strings.TrimSpace(v)
	if len(v) == 8 && isDigits(v) {
		return v[:4] + "-" + v[4:6] + "-" + v[6:8]
	}
	if len(v) == 10 && v[4] == '-' && v[7] == '-' && isDigits(v[:4]+v[5:7]+v[8:10]) {
		return v
	}
	return v
}

func isDigits(v string) bool {
	if v == "" {
		return false
	}
	for i := range v {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

func modelID(key ModelKey) string {
	if releaseID := ReleaseID(key); releaseID != "" {
		return releaseID
	}
	return LineID(key)
}
