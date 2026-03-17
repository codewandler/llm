package aggregate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/codewandler/llm"
)

var (
	// ErrUnknownAlias is returned when a model alias is not found.
	ErrUnknownAlias = errors.New("unknown model alias")
	// ErrNoProviders is returned when no providers are configured.
	ErrNoProviders = errors.New("no providers configured")
	// ErrProviderNotFound is returned when a referenced provider is not found.
	ErrProviderNotFound = errors.New("provider not found")
)

// resolveTarget resolves a target to (provider, modelID).
// It handles local alias lookup within the provider instance.
func (p *Provider) resolveTarget(target AliasTarget) (llm.Provider, string, error) {
	prov, ok := p.providers[target.Provider]
	if !ok {
		return nil, "", fmt.Errorf("%w: %s", ErrProviderNotFound, target.Provider)
	}

	// Check if target.Model is a local alias for this provider
	if localAliases, ok := p.localAliases[target.Provider]; ok {
		if modelID, ok := localAliases[target.Model]; ok {
			return prov, modelID, nil
		}
	}
	// Not a local alias, use as-is (assume it's a model ID)
	return prov, target.Model, nil
}

// resolveAllTargets resolves an alias to all possible (provider, modelID) pairs.
// Returns targets in priority order for failover.
func (p *Provider) resolveAllTargets(alias string) ([]resolvedTarget, error) {
	targets, ok := p.aliases[alias]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAlias, alias)
	}

	var resolved []resolvedTarget
	for _, target := range targets {
		prov, modelID, err := p.resolveTarget(target)
		if err != nil {
			return nil, fmt.Errorf("resolve target %s/%s: %w", target.Provider, target.Model, err)
		}
		resolved = append(resolved, resolvedTarget{
			provider: prov,
			modelID:  modelID,
			name:     target.Provider,
		})
	}

	return resolved, nil
}

type resolvedTarget struct {
	provider llm.Provider
	modelID  string
	name     string // provider instance name for error messages
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
