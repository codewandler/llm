package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/sortmap"
)

func (p *Provider) streamResponses(
	ctx context.Context, opts llm.Request,
	resolvedApiType llm.ApiType, apiKey string, pub llm.Publisher,
) {
	startTime := time.Now()

	body, err := orRespBuildRequest(opts)
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		p.opts.BaseURL+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		pub.Error(llm.NewErrBuildRequest(providerName, err))
		pub.Close()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	pub.Publish(&llm.RequestEvent{
		OriginalRequest: opts,
		ProviderRequest: llm.ProviderRequestFromHTTP(req, body),
		ResolvedApiType: resolvedApiType,
	})

	resp, err := p.client.Do(req)
	if err != nil {
		pub.Error(llm.NewErrRequestFailed(providerName, err))
		pub.Close()
		return
	}
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIError(providerName, resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			pub.Close()
			return
		}
		pub.Error(apiErr)
		pub.Close()
		return
	}

	// RespParseStream closes pub when done (via defer pub.Close() inside it).
	openai.RespParseStream(ctx, resp.Body, pub, openai.RespStreamMeta{
		RequestedModel:   opts.Model,
		StartTime:        startTime,
		ProviderName:     providerName,
		UpstreamProvider: "openai",
		Logger:           p.opts.Logger,
	})
}

// orUseResponsesAPI reports whether the bare model ID (without "openai/" prefix)
// requires the Responses API on OpenRouter. Delegates to openai.UseResponsesAPI
// — single source of truth, no duplicated model list.
func orUseResponsesAPI(bare string) bool { return openai.UseResponsesAPI(bare) }

// --- Request building ---

// orRespRequest is the top-level JSON body for OpenRouter's /v1/responses endpoint.
// Identical to openai.respRequest except there is no prompt_cache_retention field:
// OpenRouter does not expose that knob.
// TODO: consolidate with openai.respBuildRequest when the openai package exports it.
type orRespRequest struct {
	Model           string         `json:"model"`
	Input           []orRespInput  `json:"input"`
	Instructions    string         `json:"instructions,omitempty"`
	Tools           []orRespTool   `json:"tools,omitempty"`
	ToolChoice      any            `json:"tool_choice,omitempty"`
	Reasoning       *orRespReason  `json:"reasoning,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	Temperature     float64        `json:"temperature,omitempty"`
	TopP            float64        `json:"top_p,omitempty"`
	TopK            int            `json:"top_k,omitempty"`
	ResponseFormat  *orRespFormat  `json:"response_format,omitempty"`
	Stream          bool           `json:"stream"`
}

type orRespFormat struct {
	Type string `json:"type"`
}

type orRespReason struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type orRespInput struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Type      string `json:"type,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type orRespTool struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters,omitempty"`
}

// orRespBuildRequest builds the JSON body for OpenRouter's /v1/responses endpoint.
// Identical to openai.respBuildRequest except prompt_cache_retention is omitted and
// the model ID is sent as-is (OpenRouter uses the full "openai/gpt-5.4" form).
func orRespBuildRequest(opts llm.Request) ([]byte, error) {
	r := orRespRequest{
		Model:  opts.Model,
		Stream: true,
	}

	if opts.MaxTokens > 0 {
		r.MaxOutputTokens = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		r.Temperature = opts.Temperature
	}
	if opts.TopP > 0 {
		r.TopP = opts.TopP
	}
	if opts.TopK > 0 {
		r.TopK = opts.TopK
	}
	if opts.OutputFormat == llm.OutputFormatJSON {
		r.ResponseFormat = &orRespFormat{Type: "json_object"}
	}

	instructionsSet := false
	for _, m := range opts.Messages {
		switch m.Role {
		case msg.RoleSystem:
			if !instructionsSet {
				r.Instructions = m.Text()
				instructionsSet = true
			} else {
				r.Input = append(r.Input, orRespInput{Role: "developer", Content: m.Text()})
			}
		case msg.RoleUser:
			r.Input = append(r.Input, orRespInput{Role: "user", Content: m.Text()})
		case msg.RoleAssistant:
			if m.Text() != "" {
				r.Input = append(r.Input, orRespInput{Role: "assistant", Content: m.Text()})
			}
			for _, tc := range m.ToolCalls() {
				argsJSON, err := json.Marshal(tc.Args)
				if err != nil {
					return nil, fmt.Errorf("marshal tool call arguments: %w", err)
				}
				r.Input = append(r.Input, orRespInput{
					Type: "function_call", CallID: tc.ID,
					Name: tc.Name, Arguments: string(argsJSON),
				})
			}
		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				r.Input = append(r.Input, orRespInput{
					Type: "function_call_output", CallID: tr.ToolCallID, Output: tr.ToolOutput,
				})
			}
		}
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, orRespTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  sortmap.NewSortedMap(t.Parameters),
		})
	}

	if len(opts.Tools) > 0 {
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = "auto"
		case llm.ToolChoiceRequired:
			r.ToolChoice = "required"
		case llm.ToolChoiceNone:
			r.ToolChoice = "none"
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "function", "name": tc.Name}
		}
	}

	if !opts.Effort.IsEmpty() {
		r.Reasoning = &orRespReason{Effort: string(opts.Effort)}
	}

	return json.Marshal(r)
}
