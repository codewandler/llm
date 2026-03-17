package aggregate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/codewandler/llm"
)

var (
	// ErrUnknownModel is returned when a model ID or alias is not found.
	ErrUnknownModel = errors.New("unknown model")
	// ErrNoProviders is returned when no providers are configured.
	ErrNoProviders = errors.New("no providers configured")
	// ErrProviderNotFound is returned when a referenced provider is not found.
	ErrProviderNotFound = errors.New("provider not found")
	// ErrAmbiguousModel is returned when a short model ID matches multiple providers.
	ErrAmbiguousModel = errors.New("ambiguous model ID")
)

// resolvedTarget represents a fully resolved routing target.
type resolvedTarget struct {
	provider     llm.Provider
	providerName string // instance name, e.g., "work-claude"
	providerType string // provider type, e.g., "anthropic"
	modelID      string // underlying model ID, e.g., "claude-sonnet-4-5"
	fullID       string // fully qualified ID: "work-claude/anthropic/claude-sonnet-4-5"
}

// resolveTarget resolves an AliasTarget to a resolvedTarget.
func (p *Provider) resolveTarget(target AliasTarget) (resolvedTarget, error) {
	prov, ok := p.providers[target.Provider]
	if !ok {
		return resolvedTarget{}, fmt.Errorf("%w: %s", ErrProviderNotFound, target.Provider)
	}

	// Resolve local alias if present
	modelID := target.Model
	if localAliases, ok := p.localAliases[target.Provider]; ok {
		if resolved, ok := localAliases[modelID]; ok {
			modelID = resolved
		}
	}

	providerType := p.providerTypes[target.Provider]
	fullID := fmt.Sprintf("%s/%s/%s", target.Provider, providerType, modelID)

	return resolvedTarget{
		provider:     prov,
		providerName: target.Provider,
		providerType: providerType,
		modelID:      modelID,
		fullID:       fullID,
	}, nil
}

// isRetriableError checks if an error should trigger failover to the next target.
// Retriable errors: rate limits (429), service unavailable (503), quota exceeded.
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())

	// HTTP status codes
	if strings.Contains(errMsg, "429") ||
		strings.Contains(errMsg, "503") {
		return true
	}

	// Common error patterns
	retryPatterns := []string{
		"rate limit",
		"rate_limit",
		"ratelimit",
		"too many requests",
		"quota",
		"quota_exceeded",
		"quota exceeded",
		"service unavailable",
		"service_unavailable",
		"overloaded",
		"capacity",
		"temporarily unavailable",
		"try again",
	}

	for _, pattern := range retryPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	// Check for common API-specific error patterns
	retryRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(http\s+)?429\b`),
		regexp.MustCompile(`(?i)(http\s+)?503\b`),
		regexp.MustCompile(`(?i)insufficient.*quota`),
		regexp.MustCompile(`(?i)usage.*limit.*exceeded`),
		regexp.MustCompile(`(?i)request.*limit`),
	}

	for _, re := range retryRegexes {
		if re.MatchString(errMsg) {
			return true
		}
	}

	return false
}
