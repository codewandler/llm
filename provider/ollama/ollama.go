package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/sortmap"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/tool"
	"github.com/codewandler/llm/usage"
)

// Known model IDs for Ollama.
// These are tested and known to work well with the chat API.
const (
	ModelGLM47Flash      = "glm-4.7-flash"
	ModelMinistral38B    = "ministral-3:8b"
	ModelRNJ1            = "rnj-1"
	ModelFunctionGemma   = "functiongemma"
	ModelDevstralSmall2  = "devstral-small-2"
	ModelNemotron3Nano30 = "nemotron-3-nano:30b"
	ModelLlama321B       = "llama3.2:1b"
	ModelQwen317B        = "qwen3:1.7b"
	ModelQwen306B        = "qwen3:0.6b"
	ModelGranite31MoE1B  = "granite3.1-moe:1b"
	ModelQwen2505B       = "qwen2.5:0.5b"
	ModelLlama32         = "llama3.2"
	ModelLlama31         = "llama3.1"
	ModelQwen25          = "qwen2.5"
	ModelPhi3            = "phi3"
	ModelDeepSeekR1      = "deepseek-r1"
	ModelMistral         = "mistral"
	ModelGemma3          = "gemma3"
)

const (
	ModelDefault   = ModelGLM47Flash
	defaultBaseURL = "http://localhost:11434"
)

// Provider implements the Ollama (local) LLM backend.
type Provider struct {
	opts         *llm.Options
	defaultModel string
	client       *http.Client
}

// DefaultOptions returns the default options for Ollama.
// Base URL defaults to http://localhost:11434.
// No API key is required for Ollama.
func DefaultOptions() []llm.Option {
	return []llm.Option{
		llm.WithBaseURL(defaultBaseURL),
	}
}

// New creates a new Ollama provider.
// Options are applied on top of DefaultOptions().
//
// Example usage:
//
//	// Use defaults (localhost:11434)
//	p := ollama.New()
//
//	// Custom base URL
//	p := ollama.New(llm.WithBaseURL("http://remote-host:11434"))
func New(opts ...llm.Option) *Provider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	client := cfg.HTTPClient
	if client == nil {
		client = llm.DefaultHttpClient()
	}
	return &Provider{
		opts:         cfg,
		defaultModel: ModelDefault,
		client:       client,
	}
}

// WithDefaultModel sets the default model to use.
func (p *Provider) WithDefaultModel(modelID string) *Provider {
	p.defaultModel = modelID
	return p
}

// DefaultModel returns the configured default model ID.
func (p *Provider) DefaultModel() string {
	return p.defaultModel
}

func (p *Provider) Name() string { return "ollama" }

func (*Provider) CostCalculator() usage.CostCalculator {
	// Ollama is local; no cost information is available.
	return usage.CostCalculatorFunc(func(_, _ string, _ usage.TokenItems) (usage.Cost, bool) {
		return usage.Cost{}, false
	})
}

// Models returns a curated list of tested models that are known to work
// with streaming, tool calling, and conversations.
// These models are verified to be compatible with all features.
func (p *Provider) Models() []llm.Model {
	return []llm.Model{
		{ID: ModelGLM47Flash, Name: "GLM-4.7 Flash", Provider: "ollama"},
		{ID: ModelMinistral38B, Name: "Ministral 3 8B", Provider: "ollama"},
		{ID: ModelRNJ1, Name: "RNJ-1", Provider: "ollama"},
		{ID: ModelFunctionGemma, Name: "FunctionGemma", Provider: "ollama"},
		{ID: ModelDevstralSmall2, Name: "Devstral Small 2", Provider: "ollama"},
		{ID: ModelNemotron3Nano30, Name: "Nemotron 3 Nano 30B", Provider: "ollama"},
		{ID: ModelLlama321B, Name: "Llama 3.2 1B", Provider: "ollama"},
		{ID: ModelQwen317B, Name: "Qwen 3 1.7B", Provider: "ollama"},
		{ID: ModelQwen306B, Name: "Qwen 3 0.6B", Provider: "ollama"},
		{ID: ModelGranite31MoE1B, Name: "Granite 3.1 MoE 1B", Provider: "ollama"},
		{ID: ModelQwen2505B, Name: "Qwen 2.5 0.5B", Provider: "ollama"},
	}
}

// FetchModels retrieves the list of currently installed models from Ollama.
// This enumerates ALL models, including ones that may not support chat.
func (p *Provider) FetchModels(ctx context.Context) ([]llm.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.opts.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOllama, resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]llm.Model, len(result.Models))
	for i, m := range result.Models {
		models[i] = llm.Model{
			ID:       m.Name,
			Name:     m.Name,
			Provider: "ollama",
		}
	}
	return models, nil
}

// Download pulls the specified models from the Ollama registry.
// This method blocks until all models are downloaded.
// Models that are already installed will be skipped.
func (p *Provider) Download(ctx context.Context, models []llm.Model) error {
	// First, get list of installed models to skip duplicates
	installed, err := p.FetchModels(ctx)
	if err != nil {
		return fmt.Errorf("fetch installed models: %w", err)
	}

	installedMap := make(map[string]bool)
	for _, m := range installed {
		installedMap[m.ID] = true
	}

	// Download each model that isn't already installed
	for _, model := range models {
		if installedMap[model.ID] {
			continue // Skip already installed
		}

		if err := p.downloadModel(ctx, model.ID); err != nil {
			return fmt.Errorf("download %s: %w", model.ID, err)
		}
	}

	return nil
}

// downloadModel pulls a single model from the Ollama registry.
func (p *Provider) downloadModel(ctx context.Context, modelID string) error {
	reqBody := map[string]string{"name": modelID}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return llm.NewErrRequestFailed(llm.ProviderNameOllama, err)
	}
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return llm.NewErrAPIError(llm.ProviderNameOllama, resp.StatusCode, string(errBody))
	}

	// Read the streaming response until done
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var status struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &status); err != nil {
			continue // Ignore parse errors in status updates
		}

		// Check for completion or errors
		if strings.Contains(status.Status, "success") {
			return nil
		}
		if strings.Contains(strings.ToLower(status.Status), "error") {
			return fmt.Errorf("pull failed: %s", status.Status)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read pull response: %w", err)
	}

	return nil
}

func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	opts, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOllama, err)
	}

	if err := opts.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOllama, err)
	}
	body, err := buildRequest(opts)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOllama, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.opts.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameOllama, err)
	}
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameOllama, err)
	}

	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, llm.NewErrAPIError(llm.ProviderNameOllama, resp.StatusCode, string(errBody))
	}

	meta := streamMeta{
		RequestedModel: opts.Model,
		ResolvedModel:  opts.Model,
		StartTime:      startTime,
	}
	pub, ch := llm.NewEventPublisher()

	// Emit token estimates (Ollama is local, no cost charged — pass nil calculator)
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model: opts.Model, Messages: opts.Messages, Tools: opts.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, llm.ProviderNameOllama, opts.Model, "heuristic", nil) {
			pub.TokenEstimate(rec)
		}
	}

	go parseStream(ctx, resp.Body, pub, meta)
	return ch, nil
}

// --- Request building ---

type request struct {
	Model       string           `json:"model"`
	Messages    []messagePayload `json:"messages"`
	Tools       []toolPayload    `json:"tools,omitempty"`
	MaxTokens   int              `json:"num_predict,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	TopK        int              `json:"top_k,omitempty"`
	Format      string           `json:"format,omitempty"`
	Stream      bool             `json:"stream"`
}

type messagePayload struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []toolCallItem `json:"tool_calls,omitempty"`
}

type toolCallItem struct {
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolPayload struct {
	Type     string          `json:"type"`
	Function functionPayload `json:"function"`
}

type functionPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

func buildRequest(opts llm.Request) ([]byte, error) {
	r := request{
		Model:  opts.Model,
		Stream: true,
	}

	// Generation parameters
	if opts.MaxTokens > 0 {
		r.MaxTokens = opts.MaxTokens
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
		r.Format = "json"
	}

	// Note: Ollama does not support tool_choice parameter.
	// All ToolChoice settings are silently ignored (treated as auto).

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{
			Type: "function",
			Function: functionPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  sortmap.NewSortedMap(t.Parameters),
			},
		})
	}

	for _, m := range opts.Messages {
		var mp messagePayload

		switch m.Role {
		case msg.RoleSystem:
			mp = messagePayload{
				Role:    "system",
				Content: m.Text(),
			}

		case msg.RoleUser:
			mp = messagePayload{
				Role:    "user",
				Content: m.Text(),
			}

		case msg.RoleAssistant:
			mp = messagePayload{
				Role:    "assistant",
				Content: m.Text(),
			}
			for _, tc := range m.ToolCalls() {
				mp.ToolCalls = append(mp.ToolCalls, toolCallItem{
					Function: functionCall{
						Name:      tc.Name,
						Arguments: tc.Args,
					},
				})
			}

		case msg.RoleTool:
			for _, tr := range m.ToolResults() {
				mp = messagePayload{
					Role:    "tool",
					Content: tr.ToolOutput,
				}
				r.Messages = append(r.Messages, mp)
			}
			continue // avoid appending twice
		}

		r.Messages = append(r.Messages, mp)
	}

	return json.Marshal(r)
}

// --- Publisher parsing ---

// streamMeta passes context into the stream parser for StreamEventStart.
type streamMeta struct {
	RequestedModel string
	ResolvedModel  string
	StartTime      time.Time
}

type streamChunk struct {
	Message struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			Function struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"` // input tokens (final chunk only)
	EvalCount       int    `json:"eval_count,omitempty"`        // output tokens (final chunk only)
}

func parseStream(ctx context.Context, body io.ReadCloser, pub llm.Publisher, meta streamMeta) {
	defer pub.Close()
	//nolint:errcheck // intentional: defer Close is only for cleanup, failure after body consumption is non-fatal
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var toolCallID int
	startEmitted := false

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			pub.Error(llm.NewErrContextCancelled(llm.ProviderNameOllama, ctx.Err()))
			return
		default:
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			pub.Error(llm.NewErrStreamDecode(llm.ProviderNameOllama, err))
			return
		}

		if !startEmitted {
			startEmitted = true
			pub.Started(llm.StreamStartedEvent{})
		}

		if chunk.Message.Content != "" {
			pub.Delta(llm.TextDelta(chunk.Message.Content))
		}

		if len(chunk.Message.ToolCalls) > 0 {
			for _, tc := range chunk.Message.ToolCalls {
				toolCallID++
				pub.ToolCall(tool.NewToolCall(
					fmt.Sprintf("call_%d", toolCallID),
					tc.Function.Name,
					tc.Function.Arguments,
				))
			}
		}

		if chunk.Done {
			stopReason := llm.StopReasonEndTurn
			switch chunk.DoneReason {
			case "length":
				stopReason = llm.StopReasonMaxTokens
			case "stop":
				if toolCallID > 0 {
					stopReason = llm.StopReasonToolUse
				}
			default:
				if toolCallID > 0 {
					stopReason = llm.StopReasonToolUse
				}
			}
			// Build usage record from the token counts Ollama provides in the
			// final chunk. PromptEvalCount / EvalCount may be zero on older
			// Ollama builds; the record is emitted regardless so consumers
			// always receive a UsageUpdatedEvent.
			tokens := usage.TokenItems{
				{Kind: usage.KindInput, Count: chunk.PromptEvalCount},
				{Kind: usage.KindOutput, Count: chunk.EvalCount},
			}.NonZero()
			pub.UsageRecord(usage.Record{
				Dims:       usage.Dims{Provider: llm.ProviderNameOllama, Model: meta.ResolvedModel},
				Tokens:     tokens,
				RecordedAt: time.Now(),
			})
			pub.Completed(llm.CompletedEvent{StopReason: stopReason})
			return
		}
	}

	if err := scanner.Err(); err != nil {
		pub.Error(llm.NewErrStreamRead(llm.ProviderNameOllama, err))
	}
}
