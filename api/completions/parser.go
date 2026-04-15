package completions

import (
	"encoding/json"
	"fmt"

	"github.com/codewandler/llm/api/apicore"
)

// NewParser returns a ParserFactory for Chat Completions streaming payloads.
//
// Notes:
//   - The SSE event name is ignored (Chat Completions uses data-only SSE).
//   - Terminal signal is the literal StreamDone payload.
//   - Tool-call accumulation is adapter responsibility, not parser responsibility.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		return func(_ string, data []byte) apicore.StreamResult {
			if string(data) == StreamDone {
				return apicore.StreamResult{Done: true}
			}
			var chunk Chunk
			if err := json.Unmarshal(data, &chunk); err != nil {
				return apicore.StreamResult{Err: fmt.Errorf("parse chunk: %w", err)}
			}
			return apicore.StreamResult{Event: &chunk}
		}
	}
}
