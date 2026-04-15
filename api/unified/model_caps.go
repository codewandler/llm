package unified

import "strings"

// ModelCaps describes the capability set of a model that affects how
// RequestToMessages builds the wire request.
type ModelCaps struct {
	// SupportsAdaptiveThinking: model accepts thinking.type = "adaptive"
	// (claude-sonnet-4-6, claude-opus-4-6 as of 2025).
	SupportsAdaptiveThinking bool

	// SupportsEffort: model accepts output_config.effort field.
	SupportsEffort bool

	// SupportsMaxEffort: model accepts effort = "max" (subset of SupportsEffort).
	SupportsMaxEffort bool
}

// ModelCapsFunc resolves capabilities for a given model ID.
// Returning the zero-value ModelCaps is safe — all features disabled.
type ModelCapsFunc func(model string) ModelCaps

// DefaultAnthropicModelCaps is the built-in model capability resolver for
// Anthropic models. It is used by RequestToMessages when no custom resolver
// is injected via WithModelCaps.
//
// Providers with access to a live model registry can override this with a
// more accurate lookup via MessagesOption WithModelCaps.
var DefaultAnthropicModelCaps ModelCapsFunc = func(model string) ModelCaps {
	isNew := strings.Contains(model, "claude-sonnet-4-6") ||
		strings.Contains(model, "claude-opus-4-6")

	effortOnly := strings.Contains(model, "claude-opus-4-5")

	return ModelCaps{
		SupportsAdaptiveThinking: isNew,
		SupportsEffort:           isNew || effortOnly,
		SupportsMaxEffort:        isNew,
	}
}
