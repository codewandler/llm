package catalog

import (
	"fmt"
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

var openRouterCreatorMap = map[string]string{
	"ai21":                  "ai21",
	"alibaba":               "alibaba",
	"allenai":               "allenai",
	"amazon":                "amazon",
	"aion-labs":             "aion-labs",
	"anthracite-org":        "anthracite-org",
	"arcee-ai":              "arcee",
	"baidu":                 "baidu",
	"bytedance":             "bytedance",
	"bytedance-seed":        "bytedance",
	"cohere":                "cohere",
	"cognitivecomputations": "cognitivecomputations",
	"deepcogito":            "deepcogito",
	"deepseek":              "deepseek",
	"essentialai":           "essentialai",
	"google":                "google",
	"gryphe":                "gryphe",
	"ibm-granite":           "ibm",
	"inception":             "inception",
	"inflection":            "inflection",
	"kwaipilot":             "kwaipilot",
	"liquid":                "liquid",
	"mancer":                "mancer",
	"meta-llama":            "meta",
	"microsoft":             "microsoft",
	"minimax":               "minimax",
	"mistralai":             "mistral",
	"moonshotai":            "moonshot",
	"morph":                 "morph",
	"nex-agi":               "nex-agi",
	"nousresearch":          "nousresearch",
	"nvidia":                "nvidia",
	"openrouter":            "openrouter",
	"perplexity":            "perplexity",
	"prime-intellect":       "prime-intellect",
	"qwen":                  "alibaba",
	"rekaai":                "reka",
	"relace":                "relace",
	"sao10k":                "sao10k",
	"stepfun":               "stepfun",
	"switchpoint":           "switchpoint",
	"tencent":               "tencent",
	"thedrummer":            "thedrummer",
	"tngtech":               "tngtech",
	"undi95":                "undi95",
	"upstage":               "upstage",
	"writer":                "writer",
	"x-ai":                  "xai",
	"xiaomi":                "xiaomi",
	"z-ai":                  "zhipu",
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
	normalizedProvider := normalizeKeyPart(providerPart)
	switch normalizedProvider {
	case "anthropic", "openai":
		return inferModelKey(normalizedProvider, modelPart)
	case "google":
		return inferGoogleModelKey(modelPart)
	case "meta-llama":
		return inferMetaLlamaModelKey(modelPart)
	case "mistralai":
		return inferMistralModelKey(modelPart)
	case "deepseek":
		return inferDeepseekModelKey(modelPart)
	case "x-ai":
		return inferXAIModelKey(modelPart)
	case "qwen":
		return inferQwenModelKey(modelPart)
	case "nvidia":
		return inferNvidiaModelKey(modelPart)
	case "amazon":
		return inferAmazonModelKey(modelPart)
	case "z-ai":
		return inferZhipuModelKey(modelPart)
	case "baidu":
		return inferBaiduModelKey(modelPart)
	case "minimax":
		return inferMinimaxModelKey(modelPart)
	case "cohere":
		return inferCohereModelKey(modelPart)
	case "moonshotai":
		return inferMoonshotModelKey(modelPart)
	case "openrouter":
		return inferOpenRouterMetaModel(modelPart)
	default:
		creator := openRouterCreatorMap[providerPart]
		if creator == "" {
			creator = normalizedProvider
		}
		return inferGenericOpenRouterModel(creator, modelPart)
	}
}

func inferGenericOpenRouterModel(creator, modelPart string) (ModelKey, bool) {
	modelPart = strings.TrimSpace(modelPart)
	if modelPart == "" {
		return ModelKey{}, false
	}
	parts := strings.SplitN(modelPart, "-", 2)
	family := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = parts[1]
	}
	return NormalizeKey(ModelKey{Creator: creator, Family: family, Variant: variant}), true
}

func inferGoogleModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	switch {
	case strings.HasPrefix(id, "gemini-"):
		return inferGoogleGeminiKey(strings.TrimPrefix(id, "gemini-"))
	case strings.HasPrefix(id, "gemma-"):
		return inferGoogleGemmaKey(strings.TrimPrefix(id, "gemma-"))
	case strings.HasPrefix(id, "lyria-"):
		return inferGoogleLyriaKey(strings.TrimPrefix(id, "lyria-"))
	default:
		return inferGenericOpenRouterModel("google", modelPart)
	}
}

func inferGoogleGeminiKey(rest string) (ModelKey, bool) {
	releaseDate := ""
	rest = parseAndStripYYMMDateSuffix(rest, &releaseDate)
	rest = parseAndStripPartialDateSuffix(rest, &releaseDate)
	parts := strings.Split(rest, "-")
	if len(parts) == 0 {
		return ModelKey{}, false
	}
	version := parts[0]
	series := ""
	variant := ""
	if len(parts) > 1 {
		switch parts[1] {
		case "flash", "pro":
			series = parts[1]
			if len(parts) > 2 {
				variant = strings.Join(parts[2:], "-")
				variant = parseAndStripPartialDateSuffix(variant, &releaseDate)
			}
		default:
			variant = strings.Join(parts[1:], "-")
		}
	}
	return NormalizeKey(ModelKey{
		Creator:     "google",
		Family:      "gemini",
		Series:      series,
		Version:     version,
		Variant:     variant,
		ReleaseDate: releaseDate,
	}), true
}

func inferGoogleGemmaKey(rest string) (ModelKey, bool) {
	parts := strings.Split(rest, "-")
	if len(parts) == 0 {
		return ModelKey{}, false
	}
	version := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = strings.Join(parts[1:], "-")
	}
	return NormalizeKey(ModelKey{
		Creator: "google",
		Family:  "gemma",
		Version: version,
		Variant: variant,
	}), true
}

func inferGoogleLyriaKey(rest string) (ModelKey, bool) {
	parts := strings.Split(rest, "-")
	if len(parts) == 0 {
		return ModelKey{}, false
	}
	version := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = strings.Join(parts[1:], "-")
	}
	return NormalizeKey(ModelKey{
		Creator: "google",
		Family:  "lyria",
		Version: version,
		Variant: variant,
	}), true
}

func inferMetaLlamaModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if strings.HasPrefix(id, "llama-guard-") {
		rest := strings.TrimPrefix(id, "llama-guard-")
		version, variant := splitVersionAndVariant(rest)
		return NormalizeKey(ModelKey{
			Creator: "meta",
			Family:  "llama",
			Series:  "guard",
			Version: version,
			Variant: variant,
		}), true
	}
	if strings.HasPrefix(id, "llama-") {
		rest := strings.TrimPrefix(id, "llama-")
		parts := strings.SplitN(rest, "-", 2)
		if len(parts) == 0 {
			return ModelKey{}, false
		}
		version := parts[0]
		variant := ""
		if len(parts) > 1 {
			variant = parts[1]
		}
		series := ""
		if version == "4" && variant != "" {
			switch {
			case variant == "maverick" || variant == "scout":
				series = variant
				variant = ""
			case strings.HasPrefix(variant, "maverick-"):
				series = "maverick"
				variant = strings.TrimPrefix(variant, "maverick-")
			case strings.HasPrefix(variant, "scout-"):
				series = "scout"
				variant = strings.TrimPrefix(variant, "scout-")
			}
		}
		return NormalizeKey(ModelKey{
			Creator: "meta",
			Family:  "llama",
			Series:  series,
			Version: version,
			Variant: variant,
		}), true
	}
	return inferGenericOpenRouterModel("meta", modelPart)
}

func inferMistralModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	families := []string{"codestral", "devstral", "ministral", "mistral", "mixtral", "pixtral", "voxtral"}
	for _, fam := range families {
		if strings.HasPrefix(id, fam+"-") || id == fam {
			rest := strings.TrimPrefix(id, fam+"-")
			if rest == "" {
				return NormalizeKey(ModelKey{Creator: "mistral", Family: fam}), true
			}
			releaseDate := ""
			rest = parseAndStripYYMMDateSuffix(rest, &releaseDate)
			series := ""
			version := ""
			variant := ""
			switch fam {
			case "mistral", "pixtral":
				parts := strings.SplitN(rest, "-", 2)
				switch parts[0] {
				case "large", "medium", "small", "saba", "nemo", "creative":
					series = parts[0]
					if len(parts) > 1 {
						rest2 := parts[1]
						rest2 = parseAndStripYYMMDateSuffix(rest2, &releaseDate)
						vParts := strings.SplitN(rest2, "-", 2)
						version = vParts[0]
						if len(vParts) > 1 {
							variant = vParts[1]
						}
					}
				default:
					vParts := strings.SplitN(rest, "-", 2)
					version = vParts[0]
					if len(vParts) > 1 {
						variant = vParts[1]
					}
				}
			case "devstral":
				parts := strings.SplitN(rest, "-", 2)
				series = parts[0]
				if len(parts) > 1 {
					variant = parts[1]
				}
			case "ministral":
				parts := strings.SplitN(rest, "-", 2)
				variant = parts[0]
				if len(parts) > 1 {
					rest2 := parts[1]
					rest2 = parseAndStripYYMMDateSuffix(rest2, &releaseDate)
					if rest2 != "" {
						variant = joinVariantParts(variant, rest2)
					}
				}
			case "codestral":
				variant = rest
			case "mixtral":
				variant = rest
			case "voxtral":
				parts := strings.SplitN(rest, "-", 2)
				series = parts[0]
				if len(parts) > 1 {
					variant = parts[1]
				}
				variant = joinVariantParts(variant)
				variant = parseAndStripYYMMDateSuffixFromVariant(variant, &releaseDate)
			default:
				variant = rest
			}
			return NormalizeKey(ModelKey{
				Creator:     "mistral",
				Family:      fam,
				Series:      series,
				Version:     version,
				Variant:     variant,
				ReleaseDate: releaseDate,
			}), true
		}
	}
	if strings.HasPrefix(id, "mistral-7b") {
		return NormalizeKey(ModelKey{Creator: "mistral", Family: "mistral", Variant: id}), true
	}
	return inferGenericOpenRouterModel("mistral", modelPart)
}

func inferDeepseekModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "deepseek-") {
		return inferGenericOpenRouterModel("deepseek", modelPart)
	}
	rest := strings.TrimPrefix(id, "deepseek-")
	switch {
	case strings.HasPrefix(rest, "r1"):
		suffix := strings.TrimPrefix(rest, "r1")
		releaseDate := ""
		variant := ""
		if suffix != "" {
			suffix = strings.TrimPrefix(suffix, "-")
			releaseDate, variant = splitDeepseekDateAndVariant(suffix)
		}
		return NormalizeKey(ModelKey{
			Creator:     "deepseek",
			Family:      "deepseek",
			Series:      "r",
			Version:     "1",
			Variant:     variant,
			ReleaseDate: releaseDate,
		}), true
	case strings.HasPrefix(rest, "chat"):
		suffix := strings.TrimPrefix(rest, "chat")
		suffix = strings.TrimPrefix(suffix, "-")
		version := ""
		variant := ""
		releaseDate := ""
		if suffix != "" {
			if strings.HasPrefix(suffix, "v") {
				vRest := strings.TrimPrefix(suffix, "v")
				vParts := strings.SplitN(vRest, "-", 2)
				version = vParts[0]
				if len(vParts) > 1 {
					releaseDate, variant = splitDeepseekDateAndVariant(vParts[1])
				}
			} else {
				releaseDate, variant = splitDeepseekDateAndVariant(suffix)
			}
		}
		if version == "" {
			return NormalizeKey(ModelKey{Creator: "deepseek", Family: "deepseek", Variant: "chat"}), true
		}
		return NormalizeKey(ModelKey{
			Creator:     "deepseek",
			Family:      "deepseek",
			Version:     version,
			Variant:     variant,
			ReleaseDate: releaseDate,
		}), true
	case strings.HasPrefix(rest, "v"):
		vRest := strings.TrimPrefix(rest, "v")
		parts := strings.SplitN(vRest, "-", 2)
		version := parts[0]
		variant := ""
		if len(parts) > 1 {
			variant = parts[1]
		}
		return NormalizeKey(ModelKey{
			Creator: "deepseek",
			Family:  "deepseek",
			Version: version,
			Variant: variant,
		}), true
	default:
		return inferGenericOpenRouterModel("deepseek", modelPart)
	}
}

func splitDeepseekDateAndVariant(s string) (string, string) {
	if isDigits(s) && len(s) == 4 {
		// Try MMDD format first for DeepSeek (e.g., 0528 = May 28, 2025)
		mm := s[:2]
		dd := s[2:]
		if mmInt, err := strconv.Atoi(mm); err == nil && mmInt >= 1 && mmInt <= 12 {
			if ddInt, err := strconv.Atoi(dd); err == nil && ddInt >= 1 && ddInt <= 31 {
				return normalizeDate(fmt.Sprintf("2025%02s%02s", mm, dd)), ""
			}
		}
		return tryParseYYMM(s), ""
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 2 && isDigits(parts[0]) && len(parts[0]) == 4 {
		return tryParseYYMM(parts[0]), parts[1]
	}
	return "", s
}

func inferXAIModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "grok-") {
		return inferGenericOpenRouterModel("xai", modelPart)
	}
	rest := strings.TrimPrefix(id, "grok-")
	if strings.HasPrefix(rest, "code-") {
		variant := strings.TrimPrefix(rest, "code-")
		return NormalizeKey(ModelKey{
			Creator: "xai",
			Family:  "grok",
			Series:  "code",
			Variant: variant,
		}), true
	}
	parts := strings.SplitN(rest, "-", 2)
	version := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = parts[1]
	}
	return NormalizeKey(ModelKey{
		Creator: "xai",
		Family:  "grok",
		Version: version,
		Variant: variant,
	}), true
}

func inferQwenModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if strings.HasPrefix(id, "qwq") {
		rest := strings.TrimPrefix(id, "qwq")
		rest = strings.TrimPrefix(rest, "-")
		variant := rest
		return NormalizeKey(ModelKey{
			Creator: "alibaba",
			Family:  "qwq",
			Variant: variant,
		}), true
	}
	if !strings.HasPrefix(id, "qwen") {
		return inferGenericOpenRouterModel("alibaba", modelPart)
	}
	rest := strings.TrimPrefix(id, "qwen")
	series := ""
	version := ""
	variant := ""
	if rest == "" {
		return NormalizeKey(ModelKey{Creator: "alibaba", Family: "qwen"}), true
	}
	if strings.HasPrefix(rest, "-") {
		rest = rest[1:]
		seriesTemp, restTemp := splitQwenTierSeries(rest)
		if seriesTemp != "" {
			series = seriesTemp
			rest = restTemp
		}
	} else {
		// Handle direct series after qwen (no dash prefix)
		seriesTemp, restTemp := splitQwenTierSeries(rest)
		if seriesTemp != "" {
			series = seriesTemp
			rest = restTemp
		}
	}
	if rest != "" {
		if isVersionStart(rest) {
			vEnd := findVersionEnd(rest)
			version = rest[:vEnd]
			rest = rest[vEnd:]
		}
	}
	if rest != "" {
		rest = strings.TrimPrefix(rest, "-")
		variant = rest
	}
	return NormalizeKey(ModelKey{
		Creator: "alibaba",
		Family:  "qwen",
		Series:  series,
		Version: version,
		Variant: variant,
	}), true
}

func splitQwenTierSeries(rest string) (string, string) {
	tiers := []string{"vl", "coder", "max", "plus", "turbo"}
	for _, tier := range tiers {
		if rest == tier {
			return tier, ""
		}
		if strings.HasPrefix(rest, tier+"-") {
			return tier, rest[len(tier)+1:]
		}
	}
	return "", rest
}

func isVersionStart(s string) bool {
	return len(s) > 0 && (s[0] >= '0' && s[0] <= '9')
}

func findVersionEnd(s string) int {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	return i
}

func inferNvidiaModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if strings.HasPrefix(id, "llama-") {
		return inferMetaLlamaModelKey(id)
	}
	if strings.HasPrefix(id, "nematron-") {
		return inferNvidiaNemotronKey(strings.TrimPrefix(id, "nematron-"))
	}
	if strings.HasPrefix(id, "nemotron-") {
		return inferNvidiaNemotronKey(strings.TrimPrefix(id, "nemotron-"))
	}
	return inferGenericOpenRouterModel("nvidia", modelPart)
}

func inferNvidiaNemotronKey(rest string) (ModelKey, bool) {
	parts := strings.SplitN(rest, "-", 2)
	version := ""
	variant := ""
	if len(parts) >= 1 {
		if isDigits(parts[0]) {
			version = parts[0]
			if len(parts) > 1 {
				variant = parts[1]
			}
		} else {
			variant = rest
		}
	}
	return NormalizeKey(ModelKey{
		Creator: "nvidia",
		Family:  "nemotron",
		Version: version,
		Variant: variant,
	}), true
}

func inferAmazonModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "nova-") {
		return inferGenericOpenRouterModel("amazon", modelPart)
	}
	rest := strings.TrimPrefix(id, "nova-")
	parts := strings.SplitN(rest, "-", 2)
	series := ""
	version := ""
	variant := ""
	if len(parts) == 0 {
		return NormalizeKey(ModelKey{Creator: "amazon", Family: "nova"}), true
	}
	first := parts[0]
	if first == "2" && len(parts) > 1 {
		version = "2"
		rest2 := parts[1]
		seriesVariant := strings.SplitN(rest2, "-", 2)
		series = seriesVariant[0]
		if len(seriesVariant) > 1 {
			variant = seriesVariant[1]
		}
	} else {
		switch first {
		case "pro", "lite", "micro", "premier":
			series = first
			if len(parts) > 1 {
				variant = parts[1]
			}
		default:
			variant = rest
		}
	}
	return NormalizeKey(ModelKey{
		Creator: "amazon",
		Family:  "nova",
		Series:  series,
		Version: version,
		Variant: variant,
	}), true
}

func inferZhipuModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "glm-") && !strings.HasPrefix(id, "glm") {
		return inferGenericOpenRouterModel("zhipu", modelPart)
	}
	rest := id
	if strings.HasPrefix(rest, "glm-") {
		rest = strings.TrimPrefix(rest, "glm-")
	} else {
		rest = strings.TrimPrefix(rest, "glm")
	}
	version, variant := splitVersionAndVariant(rest)
	variant = joinVariantParts(variant)
	if version == "" {
		return NormalizeKey(ModelKey{Creator: "zhipu", Family: "glm", Variant: rest}), true
	}
	return NormalizeKey(ModelKey{
		Creator: "zhipu",
		Family:  "glm",
		Version: version,
		Variant: variant,
	}), true
}

func inferBaiduModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "ernie-") {
		return inferGenericOpenRouterModel("baidu", modelPart)
	}
	rest := strings.TrimPrefix(id, "ernie-")
	parts := strings.SplitN(rest, "-", 2)
	version := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = parts[1]
	}
	series := ""
	if strings.HasPrefix(variant, "vl-") {
		series = "vl"
		variant = strings.TrimPrefix(variant, "vl-")
	} else if strings.HasPrefix(variant, "vl") && len(variant) > 2 && variant[2] == '-' {
		series = "vl"
		variant = variant[3:]
	}
	return NormalizeKey(ModelKey{
		Creator: "baidu",
		Family:  "ernie",
		Series:  series,
		Version: version,
		Variant: variant,
	}), true
}

func inferMinimaxModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "minimax-") {
		return inferGenericOpenRouterModel("minimax", modelPart)
	}
	rest := strings.TrimPrefix(id, "minimax-")
	parts := strings.SplitN(rest, "-", 2)
	series := ""
	variant := ""
	if len(parts) >= 1 {
		s := parts[0]
		if len(s) > 0 && s[0] == 'm' && len(s) > 1 {
			series = "m"
			versionStr := s[1:]
			if versionStr != "" {
				variant = versionStr
			}
			if len(parts) > 1 {
				variant = joinVariantParts(variant, parts[1])
			}
			return NormalizeKey(ModelKey{
				Creator: "minimax",
				Family:  "minimax",
				Series:  series,
				Variant: variant,
			}), true
		}
		if s == "01" {
			variant = "01"
			if len(parts) > 1 {
				variant = joinVariantParts(variant, parts[1])
			}
			return NormalizeKey(ModelKey{
				Creator: "minimax",
				Family:  "minimax",
				Variant: variant,
			}), true
		}
	}
	return NormalizeKey(ModelKey{
		Creator: "minimax",
		Family:  "minimax",
		Variant: rest,
	}), true
}

func inferCohereModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "command-") {
		return inferGenericOpenRouterModel("cohere", modelPart)
	}
	rest := strings.TrimPrefix(id, "command-")
	parts := strings.SplitN(rest, "-", 2)
	series := ""
	variant := ""
	releaseDate := ""
	if len(parts) >= 1 {
		s := parts[0]
		switch s {
		case "a":
			variant = "a"
		case "r", "r7b":
			series = s
			if len(parts) > 1 {
				rest2 := parts[1]
				// Special handling for Cohere date format like "08-2024"
				rest2, releaseDate = parseAndStripCohereDateSuffix(rest2)
				if releaseDate == "" {
					rest2 = parseAndStripYYMMDateSuffix(rest2, &releaseDate)
				}
				vParts := strings.SplitN(rest2, "-", 2)
				variant = vParts[0]
				if len(vParts) > 1 {
					variant = joinVariantParts(variant, vParts[1])
				}
			}
		default:
			variant = rest
		}
	}
	return NormalizeKey(ModelKey{
		Creator:     "cohere",
		Family:      "command",
		Series:      series,
		Variant:     variant,
		ReleaseDate: releaseDate,
	}), true
}

func inferMoonshotModelKey(modelPart string) (ModelKey, bool) {
	id := strings.TrimSpace(modelPart)
	if !strings.HasPrefix(id, "kimi-") {
		return inferGenericOpenRouterModel("moonshot", modelPart)
	}
	rest := strings.TrimPrefix(id, "kimi-")
	parts := strings.SplitN(rest, "-", 2)
	versionStr := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = parts[1]
	}
	// Extract version from kX format (e.g., k2 -> 2)
	version := ""
	if strings.HasPrefix(versionStr, "k") {
		v := strings.TrimPrefix(versionStr, "k")
		if isDigits(v) {
			version = v
		} else {
			version = "2" // fallback for k2 and similar
		}
	} else {
		version = versionStr
	}
	return NormalizeKey(ModelKey{
		Creator: "moonshot",
		Family:  "kimi",
		Version: version,
		Variant: variant,
	}), true
}

func inferOpenRouterMetaModel(modelPart string) (ModelKey, bool) {
	modelPart = strings.TrimSpace(modelPart)
	if modelPart == "" {
		return ModelKey{}, false
	}
	parts := strings.SplitN(modelPart, "-", 2)
	family := parts[0]
	variant := ""
	if len(parts) > 1 {
		variant = parts[1]
	}
	return NormalizeKey(ModelKey{
		Creator: "openrouter",
		Family:  family,
		Variant: variant,
	}), true
}

func parseAndStripYYMMDateSuffix(s string, releaseDate *string) string {
	if *releaseDate != "" {
		return s
	}
	parts := strings.Split(s, "-")
	n := len(parts)
	if n >= 1 {
		last := parts[n-1]
		if isDigits(last) && len(last) == 4 {
			parsed := tryParseYYMM(last)
			if parsed != "" {
				*releaseDate = parsed
				return strings.Join(parts[:n-1], "-")
			}
		}
	}
	return s
}

func parseAndStripYYMMDateSuffixFromVariant(s string, releaseDate *string) string {
	if *releaseDate != "" {
		return s
	}
	parts := strings.Split(s, "-")
	n := len(parts)
	if n >= 1 {
		last := parts[n-1]
		if isDigits(last) && len(last) == 4 {
			parsed := tryParseYYMM(last)
			if parsed != "" {
				*releaseDate = parsed
				return strings.Join(parts[:n-1], "-")
			}
		}
	}
	return s
}

func parseAndStripCohereDateSuffix(s string) (string, string) {
	parts := strings.Split(s, "-")
	for i, part := range parts {
		if len(part) == 2 && isDigits(part) { // month
			// Check if next part exists and is a year
			if i+1 < len(parts) && len(parts[i+1]) == 4 && isDigits(parts[i+1]) {
				month := part
				year := parts[i+1]
				if monthInt, err := strconv.Atoi(month); err == nil && monthInt >= 1 && monthInt <= 12 {
					if yearInt, err := strconv.Atoi(year); err == nil && yearInt >= 2020 {
						releaseDate := fmt.Sprintf("%04d-%02d-01", yearInt, monthInt)
						result := append(parts[:i], parts[i+2:]...)
						return strings.Join(result, "-"), releaseDate
					}
				}
			}
		}
	}
	return s, ""
}

func parseAndStripPartialDateSuffix(s string, releaseDate *string) string {
	if *releaseDate != "" {
		return s
	}
	parts := strings.Split(s, "-")
	n := len(parts)
	if n >= 2 {
		last := parts[n-1]
		secondLast := parts[n-2]
		// Check for MM-YY format (Google: 05-06 -> 2006-05-01 where 06 is YY)
		if isDigits(secondLast) && len(secondLast) == 2 && isDigits(last) && len(last) == 2 {
			if mmInt, err := strconv.Atoi(secondLast); err == nil && mmInt >= 1 && mmInt <= 12 {
				if yyInt, err := strconv.Atoi(last); err == nil {
					yearInt := 2000 + yyInt
					*releaseDate = fmt.Sprintf("%04d-%02d-01", yearInt, mmInt)
					return strings.Join(parts[:n-2], "-")
				}
			}
		}
	}
	return s
}

func tryParseYYMM(s string) string {
	if len(s) != 4 || !isDigits(s) {
		return ""
	}
	yy := s[:2]
	mm := s[2:]
	year := 2000 + int(yy[0]-'0')*10 + int(yy[1]-'0')
	month := int(mm[0]-'0')*10 + int(mm[1]-'0')
	if month < 1 || month > 12 {
		return ""
	}
	return normalizeDate(fmt.Sprintf("%04d%02d01", year, month))
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
