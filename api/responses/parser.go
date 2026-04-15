package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codewandler/llm/api/apicore"
)

// toolAccum accumulates streaming function_call argument fragments.
// Populated on EventOutputItemAdded; flushed on EventOutputItemDone.
type toolAccum struct {
	callID string // external call_id for tool result messages
	name   string
	argBuf strings.Builder
}

// NewParser returns a ParserFactory for the OpenAI Responses API.
func NewParser() apicore.ParserFactory {
	return func() apicore.EventHandler {
		activeTools := make(map[int]*toolAccum) // keyed by output_index

		return func(name string, data []byte) apicore.StreamResult {
			switch name {
			case EventResponseCreated:
				var evt ResponseCreatedEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventOutputItemAdded:
				var evt OutputItemAddedEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				if evt.Item.Type == "function_call" {
					activeTools[evt.OutputIndex] = &toolAccum{
						callID: evt.Item.CallID,
						name:   evt.Item.Name,
					}
				}
				return apicore.StreamResult{Event: &evt}

			case EventReasoningDelta:
				var evt ReasoningDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventOutputTextDelta:
				var evt TextDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt}

			case EventFuncArgsDelta:
				var evt FuncArgsDeltaEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				if ta := activeTools[evt.OutputIndex]; ta != nil {
					ta.argBuf.WriteString(evt.Delta)
				}
				return apicore.StreamResult{Event: &evt}

			case EventOutputItemDone:
				var evt OutputItemDoneEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				if evt.Item.Type != "function_call" {
					return apicore.StreamResult{Event: &evt}
				}

				ta := activeTools[evt.OutputIndex]
				delete(activeTools, evt.OutputIndex)

				raw := evt.Item.Arguments
				if raw == "" && ta != nil {
					raw = ta.argBuf.String()
				}
				var args map[string]any
				if raw != "" {
					_ = json.Unmarshal([]byte(raw), &args)
				}

				callID, funcName := evt.Item.CallID, evt.Item.Name
				if ta != nil {
					if callID == "" {
						callID = ta.callID
					}
					if funcName == "" {
						funcName = ta.name
					}
				}

				return apicore.StreamResult{Event: &ToolCompleteEvent{
					ID:   callID,
					Name: funcName,
					Args: args,
				}}

			case EventResponseCompleted, EventResponseFailed:
				var evt ResponseCompletedEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{Err: fmt.Errorf("parse %s: %w", name, err)}
				}
				return apicore.StreamResult{Event: &evt, Done: true}

			case EventAPIError:
				var evt APIErrorEvent
				if err := json.Unmarshal(data, &evt); err != nil {
					return apicore.StreamResult{
						Err:  fmt.Errorf("responses API stream error (unparseable)"),
						Done: true,
					}
				}
				return apicore.StreamResult{Err: &evt, Done: true}

			default:
				// Forward-compatible: silently ignore unrecognised events.
				return apicore.StreamResult{}
			}
		}
	}
}
