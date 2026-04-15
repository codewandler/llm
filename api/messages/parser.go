package messages

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm/api/apicore"
)

type textAccum struct{ buf strings.Builder }

type thinkingAccum struct {
	thinking  strings.Builder
	signature strings.Builder
}

type toolAccum struct {
	id     string
	name   string
	argBuf strings.Builder
}

// NewParser returns a ParserFactory for the Anthropic Messages API.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		activeText := make(map[int]*textAccum)
		activeThinking := make(map[int]*thinkingAccum)
		activeTools := make(map[int]*toolAccum)

		return func(name string, data []byte) apicore.StreamResult {
			switch name {
			case EventMessageStart:
				var evt MessageStartEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse message_start: %w", err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockStart:
				var evt ContentBlockStartEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_start: %w", err)}
				}

				var block StartBlockView
				if err := json.Unmarshal(evt.ContentBlock, &block); err == nil {
					switch block.Type {
					case BlockTypeText:
						activeText[evt.Index] = &textAccum{}
					case BlockTypeThinking:
						activeThinking[evt.Index] = &thinkingAccum{}
					case BlockTypeToolUse:
						activeTools[evt.Index] = &toolAccum{id: block.ID, name: block.Name}
					case BlockTypeServerToolUse, BlockTypeWebSearchToolResult:
						// Known non-accumulating block types.
					}
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockDelta:
				var evt ContentBlockDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_delta: %w", err)}
				}

				switch evt.Delta.Type {
				case DeltaTypeText:
					if a := activeText[evt.Index]; a != nil {
						a.buf.WriteString(evt.Delta.Text)
					}
				case DeltaTypeThinking:
					if a := activeThinking[evt.Index]; a != nil {
						a.thinking.WriteString(evt.Delta.Thinking)
					}
				case DeltaTypeSignature:
					if a := activeThinking[evt.Index]; a != nil {
						a.signature.WriteString(evt.Delta.Signature)
					}
				case DeltaTypeInputJSON:
					if a := activeTools[evt.Index]; a != nil {
						a.argBuf.WriteString(evt.Delta.PartialJSON)
					}
				default:
					// Unknown future delta subtype: explicit no-op.
				}
				return apicore.StreamResult{Event: &evt}

			case EventContentBlockStop:
				var evt ContentBlockStopEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse content_block_stop: %w", err)}
				}
				idx := evt.Index

				if a, ok := activeText[idx]; ok {
					delete(activeText, idx)
					return apicore.StreamResult{Event: &TextCompleteEvent{Index: idx, Text: a.buf.String()}}
				}
				if a, ok := activeThinking[idx]; ok {
					delete(activeThinking, idx)
					return apicore.StreamResult{Event: &ThinkingCompleteEvent{
						Index: idx, Thinking: a.thinking.String(), Signature: a.signature.String(),
					}}
				}
				if a, ok := activeTools[idx]; ok {
					delete(activeTools, idx)
					var args map[string]any
					if a.argBuf.Len() > 0 {
						_ = json.Unmarshal([]byte(a.argBuf.String()), &args)
					}
					return apicore.StreamResult{Event: &ToolCompleteEvent{Index: idx, ID: a.id, Name: a.name, Args: args}}
				}

				// Known non-accumulating block stop (server_tool_use, web_search_tool_result)
				// or unknown block type: keep stop observable.
				return apicore.StreamResult{Event: &evt}

			case EventMessageDelta:
				var evt MessageDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse message_delta: %w", err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventPing:
				return apicore.StreamResult{Event: &PingEvent{}}

			case EventMessageStop:
				return apicore.StreamResult{Event: &MessageStopEvent{}, Done: true}

			case EventError:
				var evt StreamErrorEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse error event: %w", err), Done: true}
				}
				return apicore.StreamResult{Err: &evt, Done: true}

			default:
				// Forward-compatible unknown event.
				return apicore.StreamResult{}
			}
		}
	}
}
