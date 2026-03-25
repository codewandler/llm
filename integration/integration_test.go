package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/fake"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
	"github.com/codewandler/llm/tool"
)

// isClaudeAvailable checks if Claude Code credentials are available.
func isClaudeAvailable() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".claude", ".credentials.json"))
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

// isBedrockAvailable checks if AWS credentials are configured for Bedrock.
func isBedrockAvailable() bool {
	// Check environment variables
	if os.Getenv(bedrock.EnvAWSAccessKeyID) != "" {
		return true
	}
	// Check for AWS config/credentials files (including SSO)
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	// Check credentials file
	credPath := filepath.Join(home, ".aws", "credentials")
	if _, err := os.Stat(credPath); err == nil {
		return true
	}
	// Check config file (may have SSO profiles)
	configPath := filepath.Join(home, ".aws", "config")
	if _, err := os.Stat(configPath); err == nil {
		return true
	}
	return false
}

// getAWSRegion returns the configured AWS region or default.
func getAWSRegion() string {
	if region := os.Getenv(bedrock.EnvAWSRegion); region != "" {
		return region
	}
	if region := os.Getenv(bedrock.EnvAWSDefaultRegion); region != "" {
		return region
	}
	return bedrock.DefaultRegion
}

// TestProviders is a comprehensive integration test that verifies all providers
// implement the interface correctly and handle basic streaming operations.
// Add new providers to this table as they are implemented.
func TestProviders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider llm.Provider
		model    string // explicit model; empty = use provider.Models()[0].ID
		skip     bool   // Set to true for providers requiring external setup
		skipMsg  string // Reason for skipping
	}{
		{
			name:     "fake",
			provider: fake.NewProvider(),
		},
		{
			name:     "claude",
			provider: claude.New(),
			skip:     !isClaudeAvailable(),
			skipMsg:  "requires local Claude credentials (~/.claude/.credentials.json)",
		},
		{
			// Chat Completions API path
			name:     "openai/completions",
			provider: openai.New(llm.APIKeyFromEnv("OPENAI_KEY")),
			model:    openai.ModelGPT4oMini,
			skip:     os.Getenv("OPENAI_KEY") == "",
			skipMsg:  "requires OPENAI_KEY",
		},
		{
			// Responses API path (codex models route to /v1/responses)
			name:     "openai/responses",
			provider: openai.New(llm.APIKeyFromEnv("OPENAI_KEY")),
			model:    openai.ModelGPT51CodexMini,
			skip:     os.Getenv("OPENAI_KEY") == "",
			skipMsg:  "requires OPENAI_KEY",
		},
		{
			name:     "openrouter",
			provider: openrouter.New(llm.APIKeyFromEnv("OPENROUTER_API_KEY")),
			skip:     os.Getenv("OPENROUTER_API_KEY") == "",
			skipMsg:  "requires OPENROUTER_API_KEY",
		},
		{
			name:     "bedrock",
			provider: bedrock.New(bedrock.WithRegion(getAWSRegion())),
			model:    bedrock.ModelHaikuLatest,
			skip:     !isBedrockAvailable(),
			skipMsg:  "requires AWS credentials (AWS_ACCESS_KEY_ID or ~/.aws/credentials)",
		},
		{
			name:     "minimax",
			provider: minimax.New(),
			model:    minimax.ModelM27,
			skip:     os.Getenv("MINIMAX_API_KEY") == "",
			skipMsg:  "requires MINIMAX_API_KEY",
		},
		/*{
			name:     "ollama",
			provider: ollama.New(),
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

			// Resolve model: explicit override > provider default
			getModelID := func() string {
				if tt.model != "" {
					return tt.model
				}
				if tt.name == "ollama" {
					return ollama.ModelDefault
				}
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
				stream, err := tt.provider.CreateStream(ctx, llm.Request{
					Model: getModelID(),
					Messages: llm.Messages{
						llm.User("Hello"),
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
						t.Errorf("Unexpected error event: %v", event.Data.(*llm.ErrorEvent).Error)

					case llm.StreamEventCompleted:
						gotDone = true
						// Usage is optional but should be valid if present

					case llm.StreamEventDelta:
						t.Logf("Received delta: type=%s", event.Data.(*llm.DeltaEvent).Kind)
						// Valid content event

					case llm.StreamEventToolCall:
						// Valid tool call event
						assert.NotNil(t, event.Data.(*llm.ToolCallEvent).ToolCall, "StreamEventToolCall has nil ToolCall")
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

				tools := []tool.Definition{
					tool.DefinitionFor[GetWeatherParams]("get_weather", "Get the weather for a location"),
				}

				stream, err := tt.provider.CreateStream(ctx, llm.Request{
					Model: getModelID(),
					Messages: llm.Messages{
						llm.User("What's the weather?"),
					},
					Tools: tools,
				})
				require.NoError(t, err)

				// Drain and verify we get events
				eventCount := 0
				for event := range stream {
					eventCount++
					if event.Type == llm.StreamEventError {
						t.Errorf("Error event: %v", event.Data.(*llm.ErrorEvent).Error)
					}
				}

				assert.NotZero(t, eventCount, "No events received when sending with tools")
			})

			// Test 4: Multiple messages (conversation)
			t.Run("conversation", func(t *testing.T) {
				messages := llm.Messages{
					llm.System("You are a helpful assistant."),
					llm.User("Hello"),
					llm.Assistant("Hi there!"),
					llm.User("How are you?"),
				}

				stream, err := tt.provider.CreateStream(ctx, llm.Request{
					Model:    getModelID(),
					Messages: messages,
				})
				require.NoError(t, err)

				// Verify stream completes without error
				for event := range stream {
					if event.Type == llm.StreamEventError {
						t.Errorf("Unexpected error event: %v", event.Data.(*llm.ErrorEvent).Error)
					}
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

				tools := []tool.Definition{
					tool.DefinitionFor[GetWeatherParams]("get_weather", "Get the current weather for a location"),
				}

				// First request: try to get a tool call
				stream, err := tt.provider.CreateStream(ctx, llm.Request{
					Model: getModelID(),
					Messages: llm.Messages{
						llm.User("What's the weather in Paris? Use the get_weather tool."),
					},
					Tools: tools,
				})
				require.NoError(t, err)

				var toolCall tool.Call
				for event := range stream {
					if event.Type == llm.StreamEventError {
						t.Fatalf("Error in first request: %v", event.Data.(*llm.ErrorEvent).Error)
					}
					if event.Type == llm.StreamEventToolCall {
						toolCall = event.Data.(*llm.ToolCallEvent).ToolCall
						t.Logf("Received tool call: %s(%+v)", toolCall.ToolName(), toolCall.ToolArgs())
					}
				}

				// Tool calling is not guaranteed, so skip if no tool call
				if toolCall == nil {
					t.Skip("Model did not call tool (not guaranteed)")
				}

				require.NotEmpty(t, toolCall.ToolCallID(), "Tool call must have an ID")

				// Second request: send tool result back
				toolResult := `{"temperature": 22, "conditions": "sunny"}`
				stream2, err := tt.provider.CreateStream(ctx, llm.Request{
					Model: getModelID(),
					Messages: llm.Messages{
						llm.User("What's the weather in Paris? Use the get_weather tool."),
						llm.Assistant("", toolCall),
						llm.ToolResult(tool.NewResult(toolCall.ToolCallID(), toolResult, false)),
					},
					Tools: tools,
				})
				require.NoError(t, err)

				var gotResponse bool
				for event := range stream2 {
					if event.Type == llm.StreamEventError {
						t.Fatalf("Error in second request: %v", event.Data.(*llm.ErrorEvent).Error)
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

	p := ollama.New()
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
	tools := []tool.Definition{
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
				stream, err := p.CreateStream(ctx, llm.Request{
					Model: modelID,
					Messages: llm.Messages{
						llm.User("Say hello"),
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
						result.streamingError = event.Data.(*llm.ErrorEvent).Error.Error()
						t.Logf("✗ Streaming error: %v", event.Data.(*llm.ErrorEvent).Error)
						return
					case llm.StreamEventDelta:
						if de, ok := event.Data.(*llm.DeltaEvent); ok && de.Text != "" {
							gotContent = true
						}
					case llm.StreamEventCompleted:
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
				stream, err := p.CreateStream(ctx, llm.Request{
					Model: modelID,
					Messages: llm.Messages{
						llm.User("What's the weather in Paris? Use the get_weather tool."),
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
						result.toolError = event.Data.(*llm.ErrorEvent).Error.Error()
						gotError = true
						t.Logf("✗ Tool calling error: %v", event.Data.(*llm.ErrorEvent).Error)
					case llm.StreamEventToolCall:
						gotToolCall = true
						t.Logf("✓ Tool call: %s", event.Data.(*llm.ToolCallEvent).ToolCall.ToolName())
					case llm.StreamEventCompleted:
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
				messages := llm.Messages{
					llm.System("You are a helpful assistant."),
					llm.User("Hi"),
					llm.Assistant("Hello!"),
					llm.User("What's 2+2?"),
				}

				stream, err := p.CreateStream(ctx, llm.Request{
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
						result.convError = event.Data.(*llm.ErrorEvent).Error.Error()
						gotError = true
						t.Logf("✗ Conversation error: %v", event.Data.(*llm.ErrorEvent).Error)
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
