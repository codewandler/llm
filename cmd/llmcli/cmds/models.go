package cmds

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
)

// top-level aliases shown in their own section
var topLevelAliases = []string{"fast", "default", "powerful"}

// NewModelsCmd returns the models command.
func NewModelsCmd() *cobra.Command {
	var filter string

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models",
		Long: `List all models available through configured credentials.

Shows aliases first (top-level and all), then all models.
When filtering, only the models section is shown.

Examples:
  llmcli models              # List aliases and models
  llmcli models -f sonnet    # Filter models by substring`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModels(cmd.Context(), filter)
		},
	}

	cmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter models by substring (matches ID, name, aliases)")
	return cmd
}

func runModels(ctx context.Context, filter string) error {
	provider, err := createProvider(ctx)
	if err != nil {
		return err
	}

	models := provider.Models()

	// If no filter, show alias sections first
	if filter == "" {
		printAliasesSections(models)
	}

	// Filter and print models
	if filter != "" {
		models = filterModels(models, filter)
	}
	printModelsSection(models)

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

// buildAliasMap builds a map from alias -> []modelID from all models.
func buildAliasMap(models []llm.Model) map[string][]string {
	aliasMap := make(map[string][]string)
	for _, m := range models {
		for _, alias := range m.Aliases {
			aliasMap[alias] = append(aliasMap[alias], m.ID)
		}
	}
	return aliasMap
}

// isTopLevelAlias checks if the alias is one of the top-level aliases.
func isTopLevelAlias(alias string) bool {
	for _, tl := range topLevelAliases {
		if alias == tl {
			return true
		}
	}
	return false
}

// printAliasesSections prints both top-level and all aliases sections.
func printAliasesSections(models []llm.Model) {
	aliasMap := buildAliasMap(models)

	// Print top-level aliases section
	fmt.Println("ALIASES (top-level)")
	for _, alias := range topLevelAliases {
		targets, ok := aliasMap[alias]
		if !ok || len(targets) == 0 {
			continue
		}
		printAliasEntry(alias, targets)
	}
	fmt.Println()

	// Collect and sort all other aliases
	var otherAliases []string
	for alias := range aliasMap {
		if !isTopLevelAlias(alias) {
			otherAliases = append(otherAliases, alias)
		}
	}
	sort.Strings(otherAliases)

	// Print all other aliases section
	fmt.Println("ALIASES (all)")
	for _, alias := range otherAliases {
		targets := aliasMap[alias]
		printAliasEntry(alias, targets)
	}
	fmt.Println()
}

// printAliasEntry prints a single alias with its targets in multi-line format.
func printAliasEntry(alias string, targets []string) {
	fmt.Printf("  %s:\n", alias)
	for _, target := range targets {
		fmt.Printf("    %s\n", target)
	}
}

func printModelsSection(models []llm.Model) {
	if len(models) == 0 {
		fmt.Println("No models found.")
		return
	}

	fmt.Println("MODELS")

	// Calculate column width for ID
	maxID := 0
	for _, m := range models {
		if len(m.ID) > maxID {
			maxID = len(m.ID)
		}
	}

	for _, m := range models {
		fmt.Printf("  %-*s  %s\n", maxID, m.ID, m.Name)
	}
}
