// llmcli is a command-line tool for testing LLM providers.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/codewandler/llm/cmd/llmcli/cmds"
	"github.com/spf13/cobra"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	rootCmd := &cobra.Command{
		Use:   "llmcli",
		Short: "CLI tool for testing LLM providers",
		Long: `llmcli is a command-line tool for testing and demonstrating
the llm Go library's provider implementations.

Currently supports Claude OAuth authentication.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(cmds.NewAuthCmd())
	rootCmd.AddCommand(cmds.NewInferCmd())

	return rootCmd.ExecuteContext(ctx)
}
