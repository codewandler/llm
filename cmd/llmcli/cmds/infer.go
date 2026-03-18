package cmds

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
)

// NewInferCmd returns the infer command.
func NewInferCmd() *cobra.Command {
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
			return runInfer(cmd.Context(), args[0], model, system, verbose)
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "fast", "Model to use (fast, default, powerful, or full path)")
	cmd.Flags().StringVarP(&system, "system", "s", "", "System prompt to prepend")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show usage statistics")
	return cmd
}

func runInfer(ctx context.Context, message, model, system string, verbose bool) error {
	provider, err := createProvider(ctx)
	if err != nil {
		return err
	}

	msgs := llm.Messages{&llm.UserMsg{Content: message}}
	if system != "" {
		msgs = append(llm.Messages{&llm.SystemMsg{Content: system}}, msgs...)
	}

	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model:    model,
		Messages: msgs,
	})
	if err != nil {
		return fmt.Errorf("create stream: %w", err)
	}

	var startInfo *llm.StreamStart

	for event := range stream {
		switch event.Type {
		case llm.StreamEventStart:
			startInfo = event.Start
		case llm.StreamEventDelta:
			fmt.Print(event.Delta)
		case llm.StreamEventDone:
			fmt.Println()
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
	if start != nil && start.RequestID != "" {
		fields = append(fields, field{"request_id", start.RequestID})
	}

	// Requested model
	if start != nil && start.RequestedModel != "" {
		fields = append(fields, field{"requested", start.RequestedModel})
	}

	// Resolved model (only if different from requested)
	if start != nil && start.ResolvedModel != "" && start.ResolvedModel != start.RequestedModel {
		fields = append(fields, field{"resolved", start.ResolvedModel})
	}

	// API model (what the provider returned)
	if start != nil && start.ProviderModel != "" {
		fields = append(fields, field{"api_model", start.ProviderModel})
	}

	// Token usage
	if usage != nil {
		fields = append(fields, field{"tokens", fmt.Sprintf("%d in, %d out", usage.InputTokens, usage.OutputTokens)})
	}

	// Cost
	if usage != nil && usage.Cost > 0 {
		fields = append(fields, field{"cost", formatCost(usage.Cost)})
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
