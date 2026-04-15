package unified

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/tool"
)

type toolAccumulator struct {
	llm.Publisher
	active map[uint32]*accumulatedTool
}

type accumulatedTool struct {
	id   string
	name string
	args strings.Builder
}

func newLLMToolAccumulator(base llm.Publisher) *toolAccumulator {
	return &toolAccumulator{
		Publisher: base,
		active:    make(map[uint32]*accumulatedTool),
	}
}

func (p *toolAccumulator) Delta(ev *llm.DeltaEvent) {
	if ev != nil && ev.Kind == llm.DeltaKindTool && ev.Index != nil {
		idx := *ev.Index
		acc := p.active[idx]
		if acc == nil {
			acc = &accumulatedTool{}
			p.active[idx] = acc
		}
		if ev.ToolID != "" {
			acc.id = ev.ToolID
		}
		if ev.ToolName != "" {
			acc.name = ev.ToolName
		}
		if ev.ToolArgs != "" {
			acc.args.WriteString(ev.ToolArgs)
		}
	}
	p.Publisher.Delta(ev)
}

func (p *toolAccumulator) Completed(ev llm.CompletedEvent) {
	if ev.StopReason == llm.StopReasonToolUse {
		p.flushToolCalls()
	}
	p.Publisher.Completed(ev)
}

func (p *toolAccumulator) Close() {
	p.flushToolCalls()
	p.Publisher.Close()
}

func (p *toolAccumulator) flushToolCalls() {
	if len(p.active) == 0 {
		return
	}

	indices := make([]uint32, 0, len(p.active))
	for idx := range p.active {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	for _, idx := range indices {
		acc := p.active[idx]
		if acc == nil {
			continue
		}
		if acc.args.Len() == 0 && acc.id == "" && acc.name == "" {
			continue
		}

		var args map[string]any
		if acc.args.Len() > 0 {
			if err := json.Unmarshal([]byte(acc.args.String()), &args); err != nil {
				// malformed fragments; emit raw string payload
				args = map[string]any{"_raw": acc.args.String()}
			}
		}

		p.Publisher.ToolCall(tool.NewToolCall(acc.id, acc.name, args))
	}

	p.active = make(map[uint32]*accumulatedTool)
}
