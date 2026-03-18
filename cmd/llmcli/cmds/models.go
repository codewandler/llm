package cmds

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/aggregate"
)

// NewModelsCmd returns the models command.
func NewModelsCmd() *cobra.Command {
	var verbose bool
	var filter string
	var showAliases bool

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models",
		Long: `List all models available through configured credentials.

Examples:
  llmcli models                    # List all models
  llmcli models -v                 # Show model aliases
  llmcli models --show-aliases     # Show aliases section
  llmcli models -f sonnet          # Filter by substring`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModels(cmd.Context(), verbose, filter, showAliases)
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show aliases for each model")
	cmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter models by substring (matches ID, name, aliases)")
	cmd.Flags().BoolVar(&showAliases, "show-aliases", false, "Show aliases section")
	return cmd
}

func runModels(ctx context.Context, verbose bool, filter string, showAliases bool) error {
	provider, err := createProvider(ctx)
	if err != nil {
		return err
	}

	models := provider.Models()

	// Filter models
	if filter != "" {
		models = filterModels(models, filter)
	}

	// Print aliases section (if --show-aliases)
	if showAliases {
		printAliasesSection(provider, filter)
	}

	// Print models section
	printModelsSection(models, verbose)

	return nil
}

func filterModels(models []llm.Model, filter string) []llm.Model {
	filter = strings.ToLower(filter)
	filtered := models[:0]
	for _, m := range models {
		if matchesFilter(m, filter) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func matchesFilter(m llm.Model, filter string) bool {
	if strings.Contains(strings.ToLower(m.ID), filter) {
		return true
	}
	if strings.Contains(strings.ToLower(m.Name), filter) {
		return true
	}
	for _, alias := range m.Aliases {
		if strings.Contains(strings.ToLower(alias), filter) {
			return true
		}
	}
	return false
}

func printAliasesSection(provider *aggregate.Provider, filter string) {
	aliases := []string{"fast", "default", "powerful"}
	filter = strings.ToLower(filter)

	// Check if any aliases match filter
	var matchingAliases []struct {
		alias   string
		modelID string
	}

	for _, alias := range aliases {
		model, err := provider.Resolve(alias)
		if err != nil {
			continue
		}

		// Apply filter
		if filter != "" {
			if !strings.Contains(alias, filter) &&
				!strings.Contains(strings.ToLower(model.ID), filter) {
				continue
			}
		}

		matchingAliases = append(matchingAliases, struct {
			alias   string
			modelID string
		}{alias, model.ID})
	}

	if len(matchingAliases) == 0 {
		return
	}

	fmt.Println("ALIASES")
	for _, a := range matchingAliases {
		fmt.Printf("  %-12s -> %s\n", a.alias, a.modelID)
	}
	fmt.Println()
}

func printModelsSection(models []llm.Model, verbose bool) {
	if len(models) == 0 {
		fmt.Println("No models found.")
		return
	}

	fmt.Println("MODELS")

	// Calculate column widths
	maxID := 0
	maxName := 0
	for _, m := range models {
		if len(m.ID) > maxID {
			maxID = len(m.ID)
		}
		if len(m.Name) > maxName {
			maxName = len(m.Name)
		}
	}

	for _, m := range models {
		if verbose && len(m.Aliases) > 0 {
			fmt.Printf("  %-*s  %-*s  [%s]\n", maxID, m.ID, maxName, m.Name, strings.Join(m.Aliases, ", "))
		} else {
			fmt.Printf("  %-*s  %s\n", maxID, m.ID, m.Name)
		}
	}
}
