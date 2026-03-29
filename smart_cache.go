package llm

// SmartCache tracks token distance from the last cache boundary and determines
// when to mark a new conversation message as cacheable.
//
// The algorithm: after each LLM response, UpdateTokenCount accumulates the
// total input tokens seen. When the distance from the last cache mark exceeds
// Threshold, the next call to ShouldMarkForCache returns true, and MarkCachePoint
// freezes the new position. Cache marks are set on conversation messages
// (user or assistant), never on tool results.
type SmartCache struct {
	// Threshold is the minimum token distance between cache marks.
	// Disabled when <= 0.
	Threshold int

	// totalTokensSeen is the running total of input tokens from all LLM responses.
	totalTokensSeen int

	// lastCachePointAt is the totalTokensSeen value at the time of the last cache mark.
	lastCachePointAt int
}

// NewSmartCache creates a new SmartCache with the given token threshold.
// When threshold <= 0, caching is disabled.
func NewSmartCache(threshold int) *SmartCache {
	return &SmartCache{Threshold: threshold}
}

// ShouldMarkForCache returns true when the token distance from the last cache
// point exceeds the threshold. Call MarkCachePoint immediately after marking.
func (sc *SmartCache) ShouldMarkForCache() bool {
	if sc == nil || sc.Threshold <= 0 {
		return false
	}
	return sc.totalTokensSeen-sc.lastCachePointAt > sc.Threshold
}

// MarkCachePoint freezes the current token position as the new cache boundary.
// Call this immediately after applying a cache hint to a message.
func (sc *SmartCache) MarkCachePoint() {
	if sc == nil || sc.Threshold <= 0 {
		return
	}
	sc.lastCachePointAt = sc.totalTokensSeen
}

// UpdateTokenCount adds n to the running total of input tokens seen.
// Call this after each LLM response with Usage.InputTokens.
func (sc *SmartCache) UpdateTokenCount(n int) {
	if sc == nil || sc.Threshold <= 0 {
		return
	}
	sc.totalTokensSeen += n
}

// Reset clears all state. Call at the start of a new session.
func (sc *SmartCache) Reset() {
	if sc == nil {
		return
	}
	sc.totalTokensSeen = 0
	sc.lastCachePointAt = 0
}

// TotalTokensSeen returns the accumulated input token count.
func (sc *SmartCache) TotalTokensSeen() int {
	if sc == nil {
		return 0
	}
	return sc.totalTokensSeen
}

// LastCachePointAt returns the token count at the time of the last cache mark.
func (sc *SmartCache) LastCachePointAt() int {
	if sc == nil {
		return 0
	}
	return sc.lastCachePointAt
}
