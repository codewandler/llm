package llm

import (
	"strconv"
	"time"
)

// RateLimits holds parsed rate-limit headers from Anthropic API responses.
// These are emitted in the StreamStartedEvent so consumers can inspect them.
type RateLimits struct {
	// Unified limits (applies to the unified API endpoint).
	Unified *UnifiedRateLimit `json:"unified,omitempty"`

	// OrganizationID is the org this request was made under.
	OrganizationID string `json:"organization_id,omitempty"`

	// RequestID is the upstream request identifier.
	RequestID string `json:"request_id,omitempty"`
}

// UnifiedRateLimit contains the unified rate-limit data from the
// Anthropic-Ratelimit-Unified-* headers.
type UnifiedRateLimit struct {
	// The current status: "allowed", "over_budget", or "blocked".
	Status RateLimitStatus `json:"status"`

	// ResetAt is the Unix timestamp when the primary window resets.
	ResetAt time.Time `json:"reset_at"`

	// FiveHour contains limits for the 5-hour rolling window.
	FiveHour *WindowLimit `json:"five_hour,omitempty"`

	// SevenDay contains limits for the 7-day rolling window.
	SevenDay *WindowLimit `json:"seven_day,omitempty"`

	// Overage describes whether overage usage is enabled and why it might be disabled.
	Overage *OverageLimit `json:"overage,omitempty"`

	// FallbackPercentage is between 0 and 1, indicating how much of the fallback
	// (pay-as-you-go) pool is being used when the primary budget is exhausted.
	FallbackPercentage float64 `json:"fallback_percentage,omitempty"`

	// RepresentativeClaim identifies which tier/bucket this request counts against.
	RepresentativeClaim string `json:"representative_claim,omitempty"`
}

// WindowLimit represents a rate-limit window (e.g., 5-hour or 7-day).
type WindowLimit struct {
	// Status is "allowed" or "blocked".
	Status RateLimitStatus `json:"status"`

	// ResetAt is the Unix timestamp when this window resets.
	ResetAt time.Time `json:"reset_at"`

	// Utilization is between 0 and 1, representing how much of this window is used.
	Utilization float64 `json:"utilization"`
}

// OverageLimit describes the overage (pay-as-you-go) behavior.
type OverageLimit struct {
	// Status is "allowed" or "rejected".
	Status RateLimitStatus `json:"status"`

	// DisabledReason explains why overage is disabled (e.g., "out_of_credits").
	DisabledReason string `json:"disabled_reason,omitempty"`
}

// RateLimitStatus represents the status of a rate limit.
type RateLimitStatus string

const (
	RateLimitStatusAllowed    RateLimitStatus = "allowed"
	RateLimitStatusOverBudget RateLimitStatus = "over_budget"
	RateLimitStatusBlocked    RateLimitStatus = "blocked"
)

// ParseRateLimits parses rate-limit headers from an Anthropic HTTP response.
// Pass the response headers map (lowercased keys → values).
func ParseRateLimits(headers map[string]string) *RateLimits {
	rl := &RateLimits{
		OrganizationID: headers["anthropic-organization-id"],
		RequestID:      headers["request-id"],
	}

	// Check if we have unified headers
	if status := headers["anthropic-ratelimit-unified-status"]; status != "" {
		rl.Unified = &UnifiedRateLimit{
			Status: RateLimitStatus(status),
		}

		// Parse unified reset timestamp
		if reset := headers["anthropic-ratelimit-unified-reset"]; reset != "" {
			if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
				rl.Unified.ResetAt = time.Unix(ts, 0)
			}
		}

		// Parse 5-hour window
		if fStatus := headers["anthropic-ratelimit-unified-5h-status"]; fStatus != "" {
			rl.Unified.FiveHour = &WindowLimit{
				Status: RateLimitStatus(fStatus),
			}

			if reset := headers["anthropic-ratelimit-unified-5h-reset"]; reset != "" {
				if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
					rl.Unified.FiveHour.ResetAt = time.Unix(ts, 0)
				}
			}

			if util := headers["anthropic-ratelimit-unified-5h-utilization"]; util != "" {
				if f, err := strconv.ParseFloat(util, 64); err == nil {
					rl.Unified.FiveHour.Utilization = f
				}
			}
		}

		// Parse 7-day window
		if sStatus := headers["anthropic-ratelimit-unified-7d-status"]; sStatus != "" {
			rl.Unified.SevenDay = &WindowLimit{
				Status: RateLimitStatus(sStatus),
			}

			if reset := headers["anthropic-ratelimit-unified-7d-reset"]; reset != "" {
				if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
					rl.Unified.SevenDay.ResetAt = time.Unix(ts, 0)
				}
			}

			if util := headers["anthropic-ratelimit-unified-7d-utilization"]; util != "" {
				if f, err := strconv.ParseFloat(util, 64); err == nil {
					rl.Unified.SevenDay.Utilization = f
				}
			}
		}

		// Parse overage
		if oStatus := headers["anthropic-ratelimit-unified-overage-status"]; oStatus != "" {
			rl.Unified.Overage = &OverageLimit{
				Status:         RateLimitStatus(oStatus),
				DisabledReason: headers["anthropic-ratelimit-unified-overage-disabled-reason"],
			}
		}

		// Parse fallback percentage
		if fb := headers["anthropic-ratelimit-unified-fallback-percentage"]; fb != "" {
			if f, err := strconv.ParseFloat(fb, 64); err == nil {
				rl.Unified.FallbackPercentage = f
			}
		}

		// Parse representative claim
		rl.Unified.RepresentativeClaim = headers["anthropic-ratelimit-unified-representative-claim"]
	}

	return rl
}
