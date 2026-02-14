package llm_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/codewandler/llm/provider/openrouter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/fake"
	"github.com/codewandler/llm/provider/ollama"
)

// isClaudeAvailable checks if the claude CLI is available in PATH.
func isClaudeAvailable() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// isOllamaAvailable checks if Ollama is running locally.
func isOllamaAvailable() bool {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// TestProviders is a comprehensive integration test that verifies all providers
// implement the interface correctly and handle basic streaming operations.
// Add new providers to this table as they are implemented.
func TestProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider llm.Provider
		skip     bool   // Set to true for providers requiring external setup
		skipMsg  string // Reason for skipping
	}{
		{
			name:     "fake",
			provider: fake.NewProvider(),
			skip:     false,
		},
		{
			name:     "claude",
			provider: anthropic.NewClaudeCodeProvider(),
			skip:     !isClaudeAvailable(),
			skipMsg:  "requires claude CLI in PATH",
		},
		{
			name:     "openrouter",
			provider: openrouter.New(os.Getenv("OPENROUTER_API_KEY")),
			skip:     os.Getenv("OPENROUTER_API_KEY") == "",
			skipMsg:  "requires OPENROUTER_API_KEY",
		},
		/*{
			name:     "ollama",
			provider: ollama.New(""),
			skip:     !isOllamaAvailable(),
			skipMsg:  "requires ollama running on localhost:11434",
		},*/
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.skip {
				t.Skip(tt.skipMsg)
			}

			ctx := context.Background()

			// Get model ID to use for tests
			getModelID := func() string {
				// Special handling for Ollama to avoid non-chat models
				if tt.name == "ollama" {
					return ollama.ModelDefault
				}
				// Fallback to static models
				return tt.provider.Models()[0].ID
			}

			// Test 1: Provider interface methods
			t.Run("interface", func(t *testing.T) {
				// Verify Name() returns non-empty string
				assert.NotEmpty(t, tt.provider.Name(), "Name() returned empty string")

				// Verify Models() returns at least one model
				models := tt.provider.Models()
				require.NotEmpty(t, models, "Models() returned empty slice")

				// Verify each model has required fields
				for i, model := range models {
					assert.NotEmptyf(t, model.ID, "models[%d].ID is empty", i)
					assert.NotEmptyf(t, model.Name, "models[%d].Name is empty", i)
					assert.NotEmptyf(t, model.Provider, "models[%d].Provider is empty", i)
				}
			})

			// Test 2: Basic streaming
			t.Run("streaming", func(t *testing.T) {
				stream, err := tt.provider.CreateStream(ctx, llm.StreamOptions{
					Model: getModelID(),
					Messages: []llm.Message{
						{Role: llm.RoleUser, Content: "Hello"},
					},
				})
				require.NoError(t, err)

				var (
					gotAnyEvent bool
					gotDone     bool
				)

				// Consume all events
				for event := range stream {
					gotAnyEvent = true

					switch event.Type {
					case llm.StreamEventError:
						t.Errorf("Unexpected error event: %v", event.Error)

					case llm.StreamEventDone:
						gotDone = true
						// Usage is optional but should be valid if present
						if event.Usage != nil {
							assert.GreaterOrEqual(t, event.Usage.TotalTokens, 0,
								"Usage.TotalTokens is negative")
						}

					case llm.StreamEventDelta:
						t.Logf("Received delta: %s", event.Delta)
						// Valid content event

					case llm.StreamEventToolCall:
						// Valid tool call event
						assert.NotNil(t, event.ToolCall, "StreamEventToolCall has nil ToolCall")

					case llm.StreamEventReasoning:
						// Valid reasoning event
						t.Logf("Received reasoning: %s", event.Reasoning)
					}
				}

				assert.True(t, gotAnyEvent, "No events received from stream")
				assert.True(t, gotDone, "Stream did not send done event")
			})

			// Test 3: With tools
			t.Run("with_tools", func(t *testing.T) {
				type GetWeatherParams struct {
					Location string `json:"location" jsonschema:"description=City name,required"`
				}

				tools := []llm.ToolDefinition{
					llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get the weather for a location"),
				}

				stream, err := tt.provider.CreateStream(ctx, llm.StreamOptions{
					Model: getModelID(),
					Messages: []llm.Message{
						{Role: llm.RoleUser, Content: "What's the weather?"},
					},
					Tools: tools,
				})
				require.NoError(t, err)

				// Drain and verify we get events
				eventCount := 0
				for event := range stream {
					eventCount++
					if event.Type == llm.StreamEventError {
						t.Errorf("Error event: %v", event.Error)
					}
				}

				assert.NotZero(t, eventCount, "No events received when sending with tools")
			})

			// Test 4: Multiple messages (conversation)
			t.Run("conversation", func(t *testing.T) {
				messages := []llm.Message{
					{Role: llm.RoleSystem, Content: "You are a helpful assistant."},
					{Role: llm.RoleUser, Content: "Hello"},
					{Role: llm.RoleAssistant, Content: "Hi there!"},
					{Role: llm.RoleUser, Content: "How are you?"},
				}

				stream, err := tt.provider.CreateStream(ctx, llm.StreamOptions{
					Model:    getModelID(),
					Messages: messages,
				})
				require.NoError(t, err)

				// Verify stream completes without error
				for event := range stream {
					assert.NotEqual(t, llm.StreamEventError, event.Type,
						"Unexpected error event: %v", event.Error)
				}
			})

			// Test 5: Tool call round-trip
			t.Run("tool_roundtrip", func(t *testing.T) {
				// Skip for providers that don't reliably support tool calling
				if tt.name == "ollama" {
					t.Skip("Ollama tool support is model-dependent")
				}
				if tt.name == "fake" {
					t.Skip("Fake provider doesn't consume tool results")
				}

				type GetWeatherParams struct {
					Location string `json:"location" jsonschema:"description=City name,required"`
				}

				tools := []llm.ToolDefinition{
					llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get the current weather for a location"),
				}

				// First request: try to get a tool call
				stream, err := tt.provider.CreateStream(ctx, llm.StreamOptions{
					Model: getModelID(),
					Messages: []llm.Message{
						{Role: llm.RoleUser, Content: "What's the weather in Paris? Use the get_weather tool."},
					},
					Tools: tools,
				})
				require.NoError(t, err)

				var toolCall *llm.ToolCall
				for event := range stream {
					if event.Type == llm.StreamEventError {
						t.Fatalf("Error in first request: %v", event.Error)
					}
					if event.Type == llm.StreamEventToolCall {
						toolCall = event.ToolCall
						t.Logf("Received tool call: %s(%+v)", toolCall.Name, toolCall.Arguments)
					}
				}

				// Tool calling is not guaranteed, so skip if no tool call
				if toolCall == nil {
					t.Skip("Model did not call tool (not guaranteed)")
				}

				require.NotEmpty(t, toolCall.ID, "Tool call must have an ID")

				// Second request: send tool result back
				toolResult := `{"temperature": 22, "conditions": "sunny"}`
				stream2, err := tt.provider.CreateStream(ctx, llm.StreamOptions{
					Model: getModelID(),
					Messages: []llm.Message{
						{Role: llm.RoleUser, Content: "What's the weather in Paris? Use the get_weather tool."},
						{
							Role: llm.RoleAssistant,
							ToolCalls: []llm.ToolCall{
								{
									ID:        toolCall.ID,
									Name:      toolCall.Name,
									Arguments: toolCall.Arguments,
								},
							},
						},
						{
							Role:       llm.RoleTool,
							Content:    toolResult,
							ToolCallID: toolCall.ID,
						},
					},
					Tools: tools,
				})
				require.NoError(t, err)

				var gotResponse bool
				for event := range stream2 {
					if event.Type == llm.StreamEventError {
						t.Fatalf("Error in second request: %v", event.Error)
					}
					if event.Type == llm.StreamEventDelta {
						gotResponse = true
					}
				}

				assert.True(t, gotResponse, "Should get a response after tool result")
			})
		})
	}
}

// TestOllamaModels tests the curated list of Ollama models to verify they
// support streaming, tool calling, and conversations.
// Models are automatically downloaded if not already installed.
func TestOllamaModels(t *testing.T) {
	t.Skip("Ollama tests disabled")

	if !isOllamaAvailable() {
		t.Skip("requires ollama running on localhost:11434")
	}

	p := ollama.New("")
	ctx := context.Background()

	// Get the curated list of models that should work
	models := p.Models()
	require.NotEmpty(t, models, "No curated models defined")

	t.Logf("Testing %d curated models", len(models))

	// Download any models that aren't installed yet
	t.Log("Ensuring all test models are downloaded...")
	err := p.Download(ctx, models)
	if err != nil {
		t.Logf("Warning: Failed to download some models: %v", err)
		// Don't fail the test - we'll test what's available
	}

	// Define test tools
	tools := []llm.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "Get the weather for a location",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"location": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []string{"location"},
			},
		},
	}

	// Track results
	type modelResult struct {
		streaming      bool
		toolCalling    bool
		conversation   bool
		streamingError string
		toolError      string
		convError      string
	}
	results := make(map[string]*modelResult)

	for _, model := range models {
		modelID := model.ID
		result := &modelResult{}
		results[modelID] = result

		t.Run(modelID, func(t *testing.T) {
			// Test 1: Basic streaming
			t.Run("streaming", func(t *testing.T) {
				t.Parallel()
				stream, err := p.CreateStream(ctx, llm.StreamOptions{
					Model: modelID,
					Messages: []llm.Message{
						{Role: llm.RoleUser, Content: "Say hello"},
					},
				})

				if err != nil {
					result.streamingError = err.Error()
					t.Logf("✗ Streaming failed: %v", err)
					return
				}

				gotContent := false
				gotDone := false
				for event := range stream {
					switch event.Type {
					case llm.StreamEventError:
						result.streamingError = event.Error.Error()
						t.Logf("✗ Streaming error: %v", event.Error)
						return
					case llm.StreamEventDelta:
						if event.Delta != "" {
							gotContent = true
						}
					case llm.StreamEventDone:
						gotDone = true
					}
				}

				if gotContent && gotDone {
					result.streaming = true
					t.Log("✓ Streaming works")
				} else {
					result.streamingError = "no content or done event"
					t.Logf("✗ Incomplete stream (content:%v, done:%v)", gotContent, gotDone)
				}
			})

			// Test 2: Tool calling
			t.Run("tools", func(t *testing.T) {
				t.Parallel()
				stream, err := p.CreateStream(ctx, llm.StreamOptions{
					Model: modelID,
					Messages: []llm.Message{
						{Role: llm.RoleUser, Content: "What's the weather in Paris? Use the get_weather tool."},
					},
					Tools: tools,
				})

				if err != nil {
					result.toolError = err.Error()
					t.Logf("✗ Tool calling failed: %v", err)
					return
				}

				gotToolCall := false
				gotError := false
				for event := range stream {
					switch event.Type {
					case llm.StreamEventError:
						result.toolError = event.Error.Error()
						gotError = true
						t.Logf("✗ Tool calling error: %v", event.Error)
					case llm.StreamEventToolCall:
						gotToolCall = true
						t.Logf("✓ Tool call: %s", event.ToolCall.Name)
					case llm.StreamEventDone:
						// Done event received
					}
				}

				if !gotError {
					// Even if no tool call, as long as it doesn't error, mark as supported
					result.toolCalling = true
					if gotToolCall {
						t.Log("✓ Tool calling works (tool was called)")
					} else {
						t.Log("✓ Tool calling supported (no error, but tool not called)")
					}
				}
			})

			// Test 3: Conversation
			t.Run("conversation", func(t *testing.T) {
				t.Parallel()
				messages := []llm.Message{
					{Role: llm.RoleSystem, Content: "You are a helpful assistant."},
					{Role: llm.RoleUser, Content: "Hi"},
					{Role: llm.RoleAssistant, Content: "Hello!"},
					{Role: llm.RoleUser, Content: "What's 2+2?"},
				}

				stream, err := p.CreateStream(ctx, llm.StreamOptions{
					Model:    modelID,
					Messages: messages,
				})

				if err != nil {
					result.convError = err.Error()
					t.Logf("✗ Conversation failed: %v", err)
					return
				}

				gotError := false
				for event := range stream {
					if event.Type == llm.StreamEventError {
						result.convError = event.Error.Error()
						gotError = true
						t.Logf("✗ Conversation error: %v", event.Error)
						break
					}
				}

				if !gotError {
					result.conversation = true
					t.Log("✓ Conversation works")
				}
			})
		})
	}

	// Print summary
	t.Log("\n" + strings.Repeat("=", 80))
	t.Log("MODEL COMPATIBILITY SUMMARY")
	t.Log(strings.Repeat("=", 80))
	t.Logf("%-40s | Stream | Tools | Conv | Notes", "Model")
	t.Log(strings.Repeat("-", 80))

	for _, model := range models {
		result := results[model.ID]
		stream := "✗"
		tools := "✗"
		conv := "✗"

		if result.streaming {
			stream = "✓"
		}
		if result.toolCalling {
			tools = "✓"
		}
		if result.conversation {
			conv = "✓"
		}

		notes := ""
		if result.streamingError != "" {
			notes = result.streamingError
		} else if result.toolError != "" {
			notes = result.toolError
		} else if result.convError != "" {
			notes = result.convError
		}

		// Truncate model ID if too long
		modelDisplay := model.ID
		if len(modelDisplay) > 40 {
			modelDisplay = modelDisplay[:37] + "..."
		}

		if notes != "" && len(notes) > 30 {
			notes = notes[:27] + "..."
		}

		t.Logf("%-40s |   %s    |   %s   |  %s   | %s", modelDisplay, stream, tools, conv, notes)
	}
	t.Log(strings.Repeat("=", 80))

	// Report which models are fully compatible
	fullyCompatible := []string{}
	for _, model := range models {
		result := results[model.ID]
		if result.streaming && result.toolCalling && result.conversation {
			fullyCompatible = append(fullyCompatible, model.ID)
		}
	}

	if len(fullyCompatible) > 0 {
		t.Log("\nFully compatible models (all features work):")
		for _, modelID := range fullyCompatible {
			t.Logf("  - %s", modelID)
		}
	} else {
		t.Log("\nNo models are fully compatible with all features")
	}
}
