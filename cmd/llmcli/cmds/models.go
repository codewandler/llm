package cmds

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codewandler/llm"
)

// Top-level aliases shown in their own section.
var topLevelAliases = []string{"fast", "default", "powerful", "codex"}

type modelsOptions struct {
	filter     string
	provider   string
	alias      string
	allAliases bool
}

// NewModelsCmd returns the models command.
func NewModelsCmd(root *RootFlags) *cobra.Command {
	var opts modelsOptions

	cmd := &cobra.Command{
		Use:   "models",
		Short: "List available models",
		Long: `List all models available through configured credentials.

		Shows friendly aliases first, then groups models by provider.
		When filtering, only matching models are shown and aliases are printed inline.

		Examples:
		  llmcli models                     # List aliases and grouped models
		  llmcli models -f sonnet           # Filter by substring
		  llmcli models --provider openai   # Only show OpenAI models
		  llmcli models --alias sonnet      # Only models with alias 'sonnet'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModels(cmd.Context(), opts, root)
		},
	}

	cmd.Flags().StringVarP(&opts.filter, "filter", "f", "", "Filter models by substring (matches provider, ID, name, aliases)")
	cmd.Flags().StringVarP(&opts.provider, "provider", "p", "", "Filter models by provider name/path")
	cmd.Flags().StringVarP(&opts.alias, "alias", "a", "", "Filter models by alias (case-insensitive exact match)")
	cmd.Flags().BoolVar(&opts.allAliases, "all-aliases", false, "Show generated provider-prefixed aliases as well")
	return cmd
}

func runModels(ctx context.Context, opts modelsOptions, root *RootFlags) error {
	httpClient, logHandler := root.BuildHTTPClient()
	provider, err := createProvider(ctx, httpClient, root.BuildLLMOptions(logHandler)...)
	if err != nil {
		return err
	}

	models := provider.Models()
	models = filterModels(models, opts)

	if !hasModelFilters(opts) {
		printAliasesSection(models, opts.allAliases)
	}
	printModelsSection(models, opts)

	return nil
}

func hasModelFilters(opts modelsOptions) bool {
	return opts.filter != "" || opts.provider != "" || opts.alias != ""
}

func filterModels(models []llm.Model, opts modelsOptions) []llm.Model {
	filtered := make([]llm.Model, 0, len(models))
	for _, m := range models {
		if matchesModel(m, opts) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func matchesModel(m llm.Model, opts modelsOptions) bool {
	if opts.provider != "" && !matchesProvider(m, opts.provider) {
		return false
	}
	if opts.alias != "" && !matchesAlias(m, opts.alias) {
		return false
	}
	if opts.filter == "" {
		return true
	}
	filter := strings.ToLower(opts.filter)
	if strings.Contains(strings.ToLower(m.Provider), filter) {
		return true
	}
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

func matchesProvider(m llm.Model, provider string) bool {
	return strings.Contains(strings.ToLower(m.Provider), strings.ToLower(provider))
}

func matchesAlias(m llm.Model, alias string) bool {
	want := strings.ToLower(alias)
	for _, candidate := range m.Aliases {
		if strings.ToLower(candidate) == want {
			return true
		}
	}
	return false
}

// buildAliasMap builds a map from alias -> []modelID from all models.
func buildAliasMap(models []llm.Model, includeSynthetic bool) map[string][]string {
	aliasMap := make(map[string][]string)
	for _, m := range models {
		for _, alias := range displayAliases(m.Aliases, includeSynthetic) {
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

func displayAliases(aliases []string, includeSynthetic bool) []string {
	seen := make(map[string]struct{}, len(aliases))
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		if alias == "" {
			continue
		}
		if !includeSynthetic && strings.Contains(alias, "/") {
			continue
		}
		if _, ok := seen[alias]; ok {
			continue
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

// printAliasesSection prints the user-facing alias section.
func printAliasesSection(models []llm.Model, includeSynthetic bool) {
	aliasMap := buildAliasMap(models, includeSynthetic)
	fmt.Println("ALIASES")
	for _, alias := range topLevelAliases {
		targets, ok := aliasMap[alias]
		if !ok || len(targets) == 0 {
			continue
		}
		printAliasEntry(alias, targets)
	}

	var otherAliases []string
	for alias := range aliasMap {
		if !isTopLevelAlias(alias) {
			otherAliases = append(otherAliases, alias)
		}
	}
	sort.Strings(otherAliases)

	for _, alias := range otherAliases {
		targets := aliasMap[alias]
		printAliasEntry(alias, targets)
	}
	fmt.Println()
}

// printAliasEntry prints a single alias with its targets in multi-line format.
func printAliasEntry(alias string, targets []string) {
	sortedTargets := append([]string(nil), targets...)
	sort.Strings(sortedTargets)
	fmt.Printf("  %s:\n", alias)
	for _, target := range sortedTargets {
		fmt.Printf("    %s\n", target)
	}
}

type modelGroup struct {
	provider string
	models   []llm.Model
}

func groupModelsByProvider(models []llm.Model) []modelGroup {
	grouped := make(map[string][]llm.Model)
	for _, model := range models {
		grouped[model.Provider] = append(grouped[model.Provider], model)
	}
	providers := make([]string, 0, len(grouped))
	for provider := range grouped {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	out := make([]modelGroup, 0, len(providers))
	for _, provider := range providers {
		models := append([]llm.Model(nil), grouped[provider]...)
		sort.Slice(models, func(i, j int) bool {
			if models[i].ID != models[j].ID {
				return models[i].ID < models[j].ID
			}
			return models[i].Name < models[j].Name
		})
		out = append(out, modelGroup{provider: provider, models: models})
	}
	return out
}

func printModelsSection(models []llm.Model, opts modelsOptions) {
	if len(models) == 0 {
		fmt.Println("No models found.")
		return
	}

	fmt.Println("MODELS")
	for _, group := range groupModelsByProvider(models) {
		fmt.Printf("  %s (%d)\n", group.provider, len(group.models))
		maxID := 0
		for _, m := range group.models {
			if len(m.ID) > maxID {
				maxID = len(m.ID)
			}
		}
		for _, m := range group.models {
			fmt.Printf("    %-*s  %s\n", maxID, m.ID, m.Name)
			if hasModelFilters(opts) {
				aliases := displayAliases(m.Aliases, opts.allAliases)
				if len(aliases) > 0 {
					fmt.Printf("      aliases: %s\n", strings.Join(aliases, ", "))
				}
			}
		}
	}
}
