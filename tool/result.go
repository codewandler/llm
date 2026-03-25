package tool

import "encoding/json"

func NewResult(id string, output any, isError bool) Result {
	return &toolResult{
		id:      id,
		output:  output,
		isError: isError,
	}
}

type toolResult struct {
	id      string
	output  any
	isError bool
}

type wireResult struct {
	ID      string `json:"tool_call_id"`
	Output  any    `json:"output"`
	IsError bool   `json:"is_error"`
}

func (r *toolResult) ToolCallID() string { return r.id }
func (r *toolResult) ToolOutput() any    { return r.output }
func (r *toolResult) IsError() bool      { return r.isError }
func (r *toolResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireResult{r.id, r.output, r.isError})
}
func (r *toolResult) UnmarshalJSON(data []byte) error {
	var w wireResult
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	r.id = w.ID
	r.output = w.Output
	r.isError = w.IsError
	return nil
}
