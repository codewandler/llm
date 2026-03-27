package router

import (
	"errors"
	"regexp"
	"strings"

	"github.com/codewandler/llm"
)

var (
	// ErrProviderNotFound is returned when a referenced provider is not found.
	ErrProviderNotFound = errors.New("provider not found")
	// ErrAmbiguousModel is returned when a short model ID matches multiple providers.
	ErrAmbiguousModel = errors.New("ambiguous model ID")

	// ErrUnknownModel and ErrNoProviders are re-exported from the llm package
	// for backwards compatibility with callers that import from this package.
	ErrUnknownModel = llm.ErrUnknownModel
	ErrNoProviders  = llm.ErrNoProviders
)

// resolvedTarget represents a fully resolved routing target.
type resolvedTarget struct {
	provider     llm.Provider
	providerName string // instance name, e.g., "work-claude"
	providerType string // provider type, e.g., "anthropic"
	modelID      string // underlying model ID, e.g., "claude-sonnet-4-5"
	fullID       string // fully qualified ID: "work-claude/anthropic/claude-sonnet-4-5"
}

// isRetriableError checks if an error should trigger failover to the next target.
// Retriable errors: rate limits (429), service unavailable (503), quota exceeded.
func isRetriableError(pe *llm.ProviderError) bool {
	if pe == nil {
		return false
	}

	// Use the structured StatusCode field when available — no string matching needed.
	if pe.StatusCode == 429 || pe.StatusCode == 503 {
		return true
	}

	// Fall back to message heuristics. Check both Message and the full error
	// string (which includes Cause) so that wrapped plain errors are covered.
	errMsg := strings.ToLower(pe.Error())

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
