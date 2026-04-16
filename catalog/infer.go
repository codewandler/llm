package catalog

import (
	"strconv"
	"strings"
)

func inferModelKey(serviceID, rawModelID string) (ModelKey, bool) {
	serviceID = normalizeKeyPart(serviceID)
	rawModelID = strings.TrimSpace(rawModelID)
	if rawModelID == "" {
		return ModelKey{}, false
	}

	switch serviceID {
	case "anthropic":
		return inferAnthropicModelKey(rawModelID)
	case "openai":
		return inferOpenAIModelKey(rawModelID)
	case "openrouter":
		return inferOpenRouterModelKey(rawModelID)
	case "amazon-bedrock", "bedrock":
		return inferBedrockModelKey(rawModelID)
	case "ollama":
		return inferOllamaModelKey(rawModelID)
	case "dockermr":
		return inferDockerMRModelKey(rawModelID)
	default:
		return ModelKey{}, false
	}
}

func inferAnthropicModelKey(rawModelID string) (ModelKey, bool) {
	id := strings.TrimSpace(rawModelID)
	if !strings.HasPrefix(id, "claude-") {
		return ModelKey{}, false
	}
	releaseDate := ""
	parts := strings.Split(id, "-")
	if n := len(parts); n >= 2 && len(parts[n-1]) == 8 && isDigits(parts[n-1]) {
		releaseDate = normalizeDate(parts[n-1])
		parts = parts[:n-1]
	}
	if len(parts) < 3 {
		return ModelKey{}, false
	}
	parts = parts[1:]
	series := ""
	version := ""
	switch {
	case len(parts) >= 2 && isDigits(parts[0]) && isDigits(parts[1]):
		version = parts[0] + "." + parts[1]
		if len(parts) >= 3 {
			series = parts[2]
		}
	case len(parts) >= 2 && isDigits(parts[0]):
		version = parts[0]
		series = parts[1]
	default:
		series = parts[0]
		versionParts := parts[1:]
		if len(versionParts) == 1 && isDigits(versionParts[0]) {
			version = versionParts[0] + ".0"
		} else {
			version = strings.Join(versionParts, ".")
		}
	}
	if version == "" {
		return ModelKey{}, false
	}
	return NormalizeKey(ModelKey{
		Creator:     "anthropic",
		Family:      "claude",
		Series:      series,
		Version:     version,
		ReleaseDate: releaseDate,
	}), true
}

func inferOpenAIModelKey(rawModelID string) (ModelKey, bool) {
	id := strings.TrimSpace(rawModelID)
	if strings.HasPrefix(id, "gpt-") {
		rest := strings.TrimPrefix(id, "gpt-")
		parts := strings.Split(rest, "-")
		if len(parts) == 0 || parts[0] == "" {
			return ModelKey{}, false
		}
		version := parts[0]
		variant := ""
		if len(parts) > 1 {
			variant = strings.Join(parts[1:], "-")
		}
		return NormalizeKey(ModelKey{Creator: "openai", Family: "gpt", Version: version, Variant: variant}), true
	}
	if len(id) >= 2 && id[0] == 'o' {
		parts := strings.Split(strings.TrimPrefix(id, "o"), "-")
		if len(parts) == 0 || parts[0] == "" {
			return ModelKey{}, false
		}
		version := parts[0]
		variant := ""
		if len(parts) > 1 {
			variant = strings.Join(parts[1:], "-")
		}
		return NormalizeKey(ModelKey{Creator: "openai", Family: "o", Version: version, Variant: variant}), true
	}
	return ModelKey{}, false
}

func inferOpenRouterModelKey(rawModelID string) (ModelKey, bool) {
	trimmed := strings.TrimSpace(rawModelID)
	if base, _, ok := strings.Cut(trimmed, ":"); ok {
		trimmed = base
	}
	providerPart, modelPart, ok := strings.Cut(trimmed, "/")
	if !ok || providerPart == "" || modelPart == "" {
		return ModelKey{}, false
	}
	providerPart = normalizeKeyPart(providerPart)
	switch providerPart {
	case "anthropic", "openai":
		return inferModelKey(providerPart, modelPart)
	default:
		return ModelKey{}, false
	}
}

func inferBedrockModelKey(rawModelID string) (ModelKey, bool) {
	id := strings.TrimSpace(rawModelID)
	for _, prefix := range []string{"global.", "us.", "eu.", "apac."} {
		id = strings.TrimPrefix(id, prefix)
	}
	if strings.HasPrefix(id, "anthropic.") {
		anthropicID := strings.TrimPrefix(id, "anthropic.")
		anthropicID = strings.TrimSuffix(anthropicID, "-v1:0")
		anthropicID = strings.TrimSuffix(anthropicID, "-v1")
		return inferAnthropicModelKey(anthropicID)
	}
	return ModelKey{}, false
}

func inferOllamaModelKey(rawModelID string) (ModelKey, bool) {
	name, tag := splitModelAndTag(rawModelID)
	switch {
	case strings.HasPrefix(name, "llama"):
		return inferVersionedFamilyKey("meta", "llama", strings.TrimPrefix(name, "llama"), tag)
	case strings.HasPrefix(name, "qwen"):
		return inferVersionedFamilyKey("alibaba", "qwen", strings.TrimPrefix(name, "qwen"), tag)
	case strings.HasPrefix(name, "gemma"):
		return inferVersionedFamilyKey("google", "gemma", strings.TrimPrefix(name, "gemma"), tag)
	case strings.HasPrefix(name, "phi"):
		return inferVersionedFamilyKey("microsoft", "phi", strings.TrimPrefix(name, "phi"), tag)
	case strings.HasPrefix(name, "deepseek-r"):
		version := strings.TrimPrefix(name, "deepseek-r")
		return NormalizeKey(ModelKey{Creator: "deepseek", Family: "deepseek", Series: "r", Version: version, Variant: tag}), true
	case strings.HasPrefix(name, "glm-"):
		rest := strings.TrimPrefix(name, "glm-")
		version, variant := splitVersionAndVariant(rest)
		variant = joinVariantParts(variant, tag)
		if version == "" {
			return ModelKey{}, false
		}
		return NormalizeKey(ModelKey{Creator: "zhipu", Family: "glm", Version: version, Variant: variant}), true
	case strings.HasPrefix(name, "mistral"):
		version, variant := splitVersionAndVariant(strings.TrimPrefix(name, "mistral"))
		variant = joinVariantParts(variant, tag)
		return NormalizeKey(ModelKey{Creator: "mistral", Family: "mistral", Version: version, Variant: variant}), true
	case strings.HasPrefix(name, "ministral"):
		version, variant := splitVersionAndVariant(strings.TrimPrefix(name, "ministral"))
		variant = joinVariantParts(variant, tag)
		return NormalizeKey(ModelKey{Creator: "mistral", Family: "ministral", Version: version, Variant: variant}), true
	default:
		return ModelKey{}, false
	}
}

func inferDockerMRModelKey(rawModelID string) (ModelKey, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(rawModelID), "ai/")
	return inferOllamaModelKey(trimmed)
}

func provisionalLocalKey(serviceID, rawModelID string) ModelKey {
	return NormalizeKey(ModelKey{
		Creator: "local",
		Family:  normalizeOpaqueKeyPart(serviceID),
		Variant: normalizeOpaqueKeyPart(rawModelID),
	})
}

func inferModelNameFromKey(key ModelKey) string {
	key = NormalizeKey(key)
	parts := make([]string, 0, 4)
	if key.Family != "" {
		parts = append(parts, titlePart(key.Family))
	}
	if key.Series != "" {
		parts = append(parts, titlePart(key.Series))
	}
	if key.Version != "" {
		parts = append(parts, key.Version)
	}
	if key.Variant != "" {
		parts = append(parts, titlePart(key.Variant))
	}
	return strings.Join(parts, " ")
}

func titlePart(v string) string {
	v = strings.ReplaceAll(v, "-", " ")
	parts := strings.Fields(v)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		if _, err := strconv.ParseFloat(part, 64); err == nil {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func splitModelAndTag(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if base, tag, ok := strings.Cut(name, ":"); ok {
		return base, tag
	}
	return name, ""
}

func inferVersionedFamilyKey(creator, family, rest, tag string) (ModelKey, bool) {
	version, variant := splitVersionAndVariant(rest)
	variant = joinVariantParts(variant, tag)
	if version == "" && variant == "" {
		return ModelKey{}, false
	}
	return NormalizeKey(ModelKey{Creator: creator, Family: family, Version: version, Variant: variant}), true
}

func splitVersionAndVariant(rest string) (string, string) {
	rest = strings.TrimPrefix(rest, "-")
	if rest == "" {
		return "", ""
	}
	parts := strings.SplitN(rest, "-", 2)
	version := parts[0]
	variant := ""
	if len(parts) == 2 {
		variant = parts[1]
	}
	return version, variant
}

func joinVariantParts(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "-")
		if part != "" {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, "-")
}

func normalizeOpaqueKeyPart(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	replacer := strings.NewReplacer("/", "-", ":", "-", " ", "-", "_", "-", ".", "-")
	v = replacer.Replace(v)
	for strings.Contains(v, "--") {
		v = strings.ReplaceAll(v, "--", "-")
	}
	return strings.Trim(v, "-")
}
