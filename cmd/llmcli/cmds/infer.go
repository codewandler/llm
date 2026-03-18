package cmds

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/cmd/llmcli/store"
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/anthropic/claude"
)

// NewInferCmd returns the infer command.
func NewInferCmd() *cobra.Command {
	var model string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "infer <message>",
		Short: "Send a message to Claude and stream the response",
		Long: `Send a message to Claude using stored OAuth credentials.

Uses all stored credential accounts, trying each in alphabetical order
until one succeeds (useful for rate limit fallback).

Examples:
  llmcli infer "Hello, how are you?"           # Uses default model (fast/haiku)
  llmcli infer -m sonnet "Explain quantum computing"
  llmcli infer -m opus "Write a poem about Go"
  llmcli infer -m work/claude/sonnet "Hello"   # Use specific account`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfer(cmd.Context(), args[0], model, verbose)
		},
	}

	cmd.Flags().StringVarP(&model, "model", "m", "fast", "Model to use (fast, sonnet, opus, or full path)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show usage statistics")
	return cmd
}

func runInfer(ctx context.Context, message, model string, verbose bool) error {
	tokenStore, err := getTokenStore()
	if err != nil {
		return err
	}

	provider, err := buildAggregateProvider(ctx, tokenStore)
	if err != nil {
		return err
	}

	stream, err := provider.CreateStream(ctx, llm.StreamOptions{
		Model: model,
		Messages: llm.Messages{
			&llm.UserMsg{Content: message},
		},
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

func buildAggregateProvider(ctx context.Context, tokenStore *store.FileTokenStore) (*aggregate.Provider, error) {
	keys, err := tokenStore.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no credentials found; run 'llmcli auth login claude' first")
	}

	sort.Strings(keys)

	cfg := aggregate.Config{
		Name:      "llmcli",
		Providers: make([]aggregate.ProviderInstanceConfig, 0, len(keys)),
		Aliases: map[string][]aggregate.AliasTarget{
			"fast":   make([]aggregate.AliasTarget, 0, len(keys)),
			"sonnet": make([]aggregate.AliasTarget, 0, len(keys)),
			"opus":   make([]aggregate.AliasTarget, 0, len(keys)),
		},
	}

	factories := make(map[string]aggregate.Factory)

	for _, key := range keys {
		factoryKey := "claude-" + key

		cfg.Providers = append(cfg.Providers, aggregate.ProviderInstanceConfig{
			Name: key,
			Type: factoryKey,
			ModelAliases: map[string]string{
				"sonnet": "claude-sonnet-4-6",
				"opus":   "claude-opus-4-6",
				"haiku":  "claude-haiku-4-5-20251001",
			},
		})

		cfg.Aliases["fast"] = append(cfg.Aliases["fast"],
			aggregate.AliasTarget{Provider: key, Model: "haiku"})
		cfg.Aliases["sonnet"] = append(cfg.Aliases["sonnet"],
			aggregate.AliasTarget{Provider: key, Model: "sonnet"})
		cfg.Aliases["opus"] = append(cfg.Aliases["opus"],
			aggregate.AliasTarget{Provider: key, Model: "opus"})

		factories[factoryKey] = claudeFactory(key, tokenStore)
	}

	return aggregate.New(cfg, factories)
}

func claudeFactory(key string, tokenStore claude.TokenStore) aggregate.Factory {
	return func(opts ...llm.Option) llm.Provider {
		return claude.New(
			claude.WithManagedTokenProvider(key, tokenStore, nil),
		)
	}
}
