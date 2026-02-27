package main

import (
	"context"
	"fmt"
	"os"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/bedrock"
)

// Use inference profile ID for cross-region inference (required for on-demand)
const testModel = "us.anthropic.claude-3-5-haiku-20241022-v1:0"

func main() {
	ctx := context.Background()
	p := bedrock.New(bedrock.WithRegion("us-east-1"))

	fmt.Printf("Testing Bedrock provider with model: %s\n\n", testModel)

	// Test 1: Basic streaming
	fmt.Println("=== Test 1: Basic Streaming ===")
	if err := testBasicStreaming(ctx, p); err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASSED")

	// Test 2: Tool calling
	fmt.Println("\n=== Test 2: Tool Calling ===")
	if err := testToolCalling(ctx, p); err != nil {
		fmt.Fprintf(os.Stderr, "FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASSED")

	fmt.Println("\n=== All tests passed! ===")
}

func testBasicStreaming(ctx context.Context, p *bedrock.Provider) error {
	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model: testModel,
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Say hello in exactly 5 words."},
		},
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	fmt.Print("Response: ")
	for event := range stream {
		switch event.Type {
		case llm.StreamEventDelta:
			fmt.Print(event.Delta)
		case llm.StreamEventDone:
			fmt.Printf("\nTokens: %d in, %d out\n",
				event.Usage.InputTokens, event.Usage.OutputTokens)
		case llm.StreamEventError:
			return event.Error
		}
	}
	return nil
}

type GetWeatherParams struct {
	Location string `json:"location" jsonschema:"description=City name,required"`
}

func testToolCalling(ctx context.Context, p *bedrock.Provider) error {
	tools := []llm.ToolDefinition{
		llm.ToolDefinitionFor[GetWeatherParams]("get_weather", "Get weather for a city"),
	}

	stream, err := p.CreateStream(ctx, llm.StreamOptions{
		Model: testModel,
		Messages: llm.Messages{
			&llm.UserMsg{Content: "What's the weather in Paris? Use the get_weather tool."},
		},
		Tools:      tools,
		ToolChoice: llm.ToolChoiceRequired{},
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	var toolCall *llm.ToolCall
	for event := range stream {
		switch event.Type {
		case llm.StreamEventToolCall:
			toolCall = event.ToolCall
			fmt.Printf("Tool call: %s(location=%v)\n", toolCall.Name, toolCall.Arguments["location"])
		case llm.StreamEventDone:
			fmt.Printf("Tokens: %d in, %d out\n",
				event.Usage.InputTokens, event.Usage.OutputTokens)
		case llm.StreamEventError:
			return event.Error
		}
	}

	if toolCall == nil {
		return fmt.Errorf("expected tool call, got none")
	}
	if toolCall.Name != "get_weather" {
		return fmt.Errorf("expected get_weather, got %s", toolCall.Name)
	}
	return nil
}
