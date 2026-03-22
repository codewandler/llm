package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
)

// NewInferCmd returns the infer command.
func NewInferCmd(root *RootFlags) *cobra.Command {
	var model string
	var system string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "infer <message>",
		Short: "Send a message to Claude and stream the response",
		Long: `Send a message to Claude using stored OAuth credentials.

Uses all stored credential accounts, trying each in alphabetical order
until one succeeds (useful for rate limit fallback).

Examples:
  llmcli infer "Hello, how are you?"              # Uses fast model (haiku)
  llmcli infer -m default "Explain Go channels"   # Balanced (sonnet)
  llmcli infer -m powerful "Write a poem about Go" # Most capable (opus)
  llmcli infer -s "You are a pirate" "Hello"      # With system prompt
  llmcli infer -m work/claude/sonnet "Hello"      # Use specific account`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfer(cmd.Context(), args[0], model, system, verbose, root)
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "fast", "Model to use (fast, default, powerful, or full path)")
	cmd.Flags().StringVarP(&system, "system", "s", "", "System prompt to prepend")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show usage statistics")
	return cmd
}

func runInfer(ctx context.Context, message, model, system string, verbose bool, root *RootFlags) error {
	httpClient, logHandler := root.BuildHTTPClient()
	provider, err := createProvider(ctx, httpClient, root.BuildLLMOptions(logHandler)...)
	if err != nil {
		return err
	}

	msgs := llm.Messages{&llm.UserMsg{Content: message}}
	if system != "" {
		msgs = append(llm.Messages{&llm.SystemMsg{Content: system}}, msgs...)
	}

	stream, err := provider.CreateStream(ctx, llm.StreamRequest{
		Model:    model,
		Messages: msgs,
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	var startInfo *llm.StreamStart
	var hadTokenOutput bool

	for event := range stream {
		if root.LogEvents {
			logStreamEvent(event)
		}
		switch event.Type {
		case llm.StreamEventStart:
			startInfo = event.Start
		case llm.StreamEventDelta:
			fmt.Print(event.Delta)
			if logHandler != nil {
				hadTokenOutput = true
				logHandler.MarkTokenOutput()
			}
		case llm.StreamEventDone:
			if hadTokenOutput {
				fmt.Println()
			}
			if verbose {
				printVerboseInfo(startInfo, event.Usage)
			}
		case llm.StreamEventError:
			return event.Error
		}
	}

	return nil
}

// printVerboseInfo prints multi-line verbose metadata with right-aligned labels.
func printVerboseInfo(start *llm.StreamStart, usage *llm.Usage) {
	type field struct {
		label string
		value string
	}
	var fields []field

	// Request ID
	if start != nil && start.ProviderRequestID != "" {
		fields = append(fields, field{"request_id", start.ProviderRequestID})
	}

	// Requested model
	if start != nil && start.ModelRequested != "" {
		fields = append(fields, field{"requested", start.ModelRequested})
	}

	// Resolved model (only if different from requested)
	if start != nil && start.ModelResolved != "" && start.ModelResolved != start.ModelRequested {
		fields = append(fields, field{"resolved", start.ModelResolved})
	}

	// API model (what the provider returned)
	if start != nil && start.ModelProviderID != "" {
		fields = append(fields, field{"api_model", start.ModelProviderID})
	}

	// Token usage
	if usage != nil {
		fields = append(fields, field{"tokens", fmt.Sprintf("%d in, %d out", usage.InputTokens, usage.OutputTokens)})
	}

	// Cache usage (shown only when provider returned cache data)
	if usage != nil && (usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0) {
		fields = append(fields, field{"cache", fmt.Sprintf("%d read, %d written", usage.CacheReadTokens, usage.CacheWriteTokens)})
	}

	// Cost
	if usage != nil && usage.Cost > 0 {
		hasBreakdown := usage.InputCost > 0 || usage.CacheReadCost > 0 || usage.CacheWriteCost > 0
		if hasBreakdown {
			fields = append(fields, field{"cost", fmt.Sprintf("%s (in %s, cache-r %s, cache-w %s, out %s)",
				formatCost(usage.Cost),
				formatCost(usage.InputCost),
				formatCost(usage.CacheReadCost),
				formatCost(usage.CacheWriteCost),
				formatCost(usage.OutputCost),
			)})
		} else {
			fields = append(fields, field{"cost", formatCost(usage.Cost)})
		}
	}

	// Time to first token
	if start != nil && start.TimeToFirstToken > 0 {
		fields = append(fields, field{"ttft", fmt.Sprintf("%dms", start.TimeToFirstToken.Milliseconds())})
	}

	if len(fields) == 0 {
		return
	}

	// Calculate max label width for alignment
	maxWidth := 0
	for _, f := range fields {
		if len(f.label) > maxWidth {
			maxWidth = len(f.label)
		}
	}

	// Print with right-aligned labels
	fmt.Println()
	for _, f := range fields {
		fmt.Printf("%*s: %s\n", maxWidth, f.label, f.value)
	}
}

// formatCost formats cost with appropriate precision for the amount.
// Smaller costs get more decimal places for visibility.
func formatCost(cost float64) string {
	switch {
	case cost < 0.0001:
		return fmt.Sprintf("$%.8f", cost)
	case cost < 0.01:
		return fmt.Sprintf("$%.6f", cost)
	case cost < 1.0:
		return fmt.Sprintf("$%.4f", cost)
	default:
		return fmt.Sprintf("$%.2f", cost)
	}
}

// noisyStreamEvents are collapsed to a single header line with no body.
var noisyStreamEvents = map[llm.StreamEventType]bool{
	llm.StreamEventCreated:   true,
	llm.StreamEventDelta:     true,
	llm.StreamEventReasoning: true,
}

// logStreamEvent pretty-prints a StreamEvent to stderr using the same visual
// style as the SSE event renderer in httplog.go: bold [event_type] header
// followed by indented JSON body. Delta and reasoning events are collapsed to
// a single line (they are too numerous to expand usefully).
func logStreamEvent(ev llm.StreamEvent) {
	eventType := string(ev.Type)

	if noisyStreamEvents[ev.Type] {
		fmt.Fprintf(os.Stderr, "\n    %s[%s]%s\n", ansiBold, eventType, ansiReset)
		return
	}

	// StreamEvent marshals cleanly except for the Error field (error interface
	// is not JSON-serialisable). Use the type alias trick to shadow it with a
	// string while keeping all other field tags from the struct.
	type streamEventAlias llm.StreamEvent
	b, err := json.Marshal(struct {
		streamEventAlias
		Error string `json:"error,omitempty"`
	}{
		streamEventAlias: streamEventAlias(ev),
		Error:            func() string {
			if ev.Error != nil {
				return ev.Error.Error()
			}
			return ""
		}(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n    %s[%s]%s\n    (marshal error: %v)\n", ansiBold, eventType, ansiReset, err)
		return
	}

	fmt.Fprintf(os.Stderr, "\n    %s[%s]%s\n", ansiBold, eventType, ansiReset)
	fmt.Fprintln(os.Stderr, indentAll(prettyJSON(string(b)), "    "))
}
