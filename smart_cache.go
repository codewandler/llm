package llm

// SmartCache tracks cache points and automatically marks messages for caching based on token distance.
// When distance from the last cache point exceeds the threshold, the next user message is marked cacheable.
type SmartCache struct {
	// Threshold in tokens. When distance from LastCachePointTokens exceeds this,
	// the next user message will be marked for caching. Disabled when <= 0.
	Threshold int

	// LastCachePointTokens tracks where the last cache point was set.
	// Updated after each LLM response using the input token count.
	LastCachePointTokens int
}

// NewSmartCache creates a new smart cache with the given threshold.
// When threshold <= 0, caching is disabled.
func NewSmartCache(threshold int) *SmartCache {
	return &SmartCache{
		Threshold:            threshold,
		LastCachePointTokens: 0,
	}
}

// ShouldMarkForCache checks if the distance from the last cache point exceeds the threshold.
// Returns true if the next message should be marked cacheable.
func (sc *SmartCache) ShouldMarkForCache(currentTotalTokens int) bool {
	if sc == nil || sc.Threshold <= 0 {
		return false
	}
	distance := currentTotalTokens - sc.LastCachePointTokens
	return distance > sc.Threshold
}

// MarkCachePoint records the current token position as a cache point.
// Call this after a message is marked cacheable.
func (sc *SmartCache) MarkCachePoint(currentTotalTokens int) {
	if sc == nil || sc.Threshold <= 0 {
		return
	}
	sc.LastCachePointTokens = currentTotalTokens
}

// UpdateTokenCount updates the running token count from a usage report.
// This is called after each LLM response to track cumulative input tokens.
func (sc *SmartCache) UpdateTokenCount(inputTokens int) {
	if sc == nil || sc.Threshold <= 0 {
		return
	}
	sc.LastCachePointTokens += inputTokens
}

// Reset clears the cache point tracking.
// Call this at the start of a new session if you want to reset the cache state.
func (sc *SmartCache) Reset() {
	if sc == nil {
		return
	}
	sc.LastCachePointTokens = 0
}
