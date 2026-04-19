// llmcli is a command-line tool for testing LLM providers.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	modeldb "github.com/codewandler/modeldb"
	modeldbcli "github.com/codewandler/modeldb/cli"
	"github.com/spf13/cobra"

	"github.com/codewandler/llm/cmd/llmcli/cmds"
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
	rootFlags := &cmds.RootFlags{}

	rootCmd := &cobra.Command{
		Use:   "llmcli",
		Short: "LLM CLI tool",
		Long: `A CLI tool for interacting with LLM providers.
Currently supports Claude OAuth authentication.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.PersistentFlags().BoolVar(&rootFlags.Debug, "debug", false, "Enable provider debug logging")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.LogHTTP, "log-http", false, "Log HTTP requests and responses at debug level")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.LogHTTPDebug, "log-http-debug", false, "Log HTTP headers and bodies (implies --log-http)")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.LogHTTPAllHeaders, "log-http-all-headers", false, "Show all response headers instead of curated list (implies --log-http-debug)")
	rootCmd.PersistentFlags().BoolVar(&rootFlags.LogEvents, "log-events", false, "Log each StreamEvent as JSON to stderr as it is received")

	rootCmd.AddCommand(cmds.NewAuthCmd())
	rootCmd.AddCommand(cmds.NewClaudeCmd())
	rootCmd.AddCommand(cmds.NewInferCmd(rootFlags))
	rootCmd.AddCommand(modeldbcli.NewModelsCommand(modeldbcli.ModelsCommandOptions{LoadBaseCatalog: func(ctx context.Context) (modeldb.Catalog, error) { return modelcatalog.LoadMergedBuiltIn() }}))

	return rootCmd.ExecuteContext(ctx)
}
