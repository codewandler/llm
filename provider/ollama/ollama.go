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

	"github.com/codewandler/llm"
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

const ModelDefault = ModelGLM47Flash

// Provider implements the Ollama (local) LLM backend.
type Provider struct {
	baseURL      string
	defaultModel string
	client       *http.Client
}

// New creates a new Ollama provider.
// If baseURL is empty, defaults to http://localhost:11434.
func New(baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Provider{
		baseURL:      baseURL,
		defaultModel: ModelDefault,
		client:       &http.Client{},
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
	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama models API failed (HTTP %d): %s", resp.StatusCode, string(body))
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

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pull request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull API error (HTTP %d): %s", resp.StatusCode, string(errBody))
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

func (p *Provider) SendMessage(ctx context.Context, opts llm.SendOptions) (<-chan llm.StreamEvent, error) {
	body, err := buildRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go parseStream(resp.Body, events)
	return events, nil
}

// --- Request building ---

type request struct {
	Model    string           `json:"model"`
	Messages []messagePayload `json:"messages"`
	Tools    []toolPayload    `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
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
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func buildRequest(opts llm.SendOptions) ([]byte, error) {
	r := request{
		Model:  opts.Model,
		Stream: true,
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{
			Type: "function",
			Function: functionPayload{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	for _, msg := range opts.Messages {
		m := messagePayload{
			Role:    string(msg.Role),
			Content: msg.Content,
		}

		// Convert tool calls if present
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, toolCallItem{
					Function: functionCall{
						Name:      tc.Name,
						Arguments: tc.Arguments,
					},
				})
			}
		}

		r.Messages = append(r.Messages, m)
	}

	return json.Marshal(r)
}

// --- Stream parsing ---

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
	Done bool `json:"done"`
}

func parseStream(body io.ReadCloser, events chan<- llm.StreamEvent) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var usage llm.Usage
	toolCallID := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventError,
				Error: fmt.Errorf("parse chunk: %w", err),
			}
			return
		}

		// Handle content delta
		if chunk.Message.Content != "" {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDelta,
				Delta: chunk.Message.Content,
			}
		}

		// Handle tool calls
		if len(chunk.Message.ToolCalls) > 0 {
			for _, tc := range chunk.Message.ToolCalls {
				toolCallID++
				events <- llm.StreamEvent{
					Type: llm.StreamEventToolCall,
					ToolCall: &llm.ToolCall{
						ID:        fmt.Sprintf("call_%d", toolCallID),
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}

		// Handle done
		if chunk.Done {
			events <- llm.StreamEvent{
				Type:  llm.StreamEventDone,
				Usage: &usage,
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		events <- llm.StreamEvent{
			Type:  llm.StreamEventError,
			Error: fmt.Errorf("scan stream: %w", err),
		}
	}
}
