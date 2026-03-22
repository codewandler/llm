package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/codewandler/llm"
)

type contentBlockStartEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"content_block"`
}

type contentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

// StreamMeta passes context into the stream parser for StreamEventStart.
type StreamMeta struct {
	RequestedModel string
	ResolvedModel  string
	StartTime      time.Time
}

// ParseStream parses an Anthropic SSE stream and sends events to the stream.
func ParseStream(ctx context.Context, body io.ReadCloser, events *llm.EventStream, meta StreamMeta) {
	defer events.Close()
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	type toolBlock struct {
		id      string
		name    string
		jsonBuf strings.Builder
	}
	activeTools := make(map[int]*toolBlock)
	var usage llm.Usage

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			events.Error(llm.NewErrContextCancelled(llm.ProviderNameAnthropic, ctx.Err()))
			return
		default:
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &base); err != nil {
			continue
		}

		switch base.Type {
		case "message_start":
			var evt struct {
				Message struct {
					ID    string `json:"id"`
					Model string `json:"model"`
					Usage struct {
						InputTokens              int `json:"input_tokens"`
						CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
						CacheReadInputTokens     int `json:"cache_read_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				usage.CacheWriteTokens = evt.Message.Usage.CacheCreationInputTokens
				usage.CacheReadTokens = evt.Message.Usage.CacheReadInputTokens
				// InputTokens is total input: uncached remainder + cache-read + cache-write.
				// The wire field input_tokens is only the uncached remainder (tokens after
				// the last cache breakpoint), so we sum all three buckets here.
				usage.InputTokens = evt.Message.Usage.InputTokens +
					usage.CacheWriteTokens + usage.CacheReadTokens

				// Emit StreamEventStart with metadata
				events.Start(llm.StreamStartOpts{
					Model:     evt.Message.Model,
					RequestID: evt.Message.ID,
				})
			}

		case "message_delta":
			var evt struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				usage.OutputTokens = evt.Usage.OutputTokens
				usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			}

		case "content_block_start":
			var evt contentBlockStartEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			if evt.ContentBlock.Type == "tool_use" {
				activeTools[evt.Index] = &toolBlock{id: evt.ContentBlock.ID, name: evt.ContentBlock.Name}
			}

		case "content_block_delta":
			var evt contentBlockDeltaEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			switch evt.Delta.Type {
			case "text_delta":
				events.Delta(evt.Delta.Text)
			case "input_json_delta":
				if tb, ok := activeTools[evt.Index]; ok {
					tb.jsonBuf.WriteString(evt.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			var evt struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			if tb, ok := activeTools[evt.Index]; ok {
				var args map[string]any
				if tb.jsonBuf.Len() > 0 {
					_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
				}
				events.ToolCall(llm.ToolCall{ID: tb.id, Name: tb.name, Arguments: args})
				delete(activeTools, evt.Index)
			}

		case "message_stop":
			FillCost(meta.ResolvedModel, &usage)
			events.Done(&usage)
			return

		case "error":
			var errEvt struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errEvt); err == nil {
				events.Error(llm.NewErrProviderMsg(llm.ProviderNameAnthropic, errEvt.Error.Message))
			}
			return
		}
	}
}
