package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/codewandler/llm/catalog"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "build":
		build(os.Args[2:])
	case "validate":
		validate(os.Args[2:])
	case "inspect":
		inspect(os.Args[2:])
	case "model":
		model(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func model(args []string) {
	if len(args) < 1 {
		modelUsage()
		os.Exit(2)
	}
	switch args[0] {
	case "show":
		modelShow(args[1:])
	default:
		modelUsage()
		os.Exit(2)
	}

}

func build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outPath := fs.String("out", "catalog.json", "output catalog JSON path")
	anthropicFile := fs.String("anthropic-file", "", "optional local Anthropics models payload path")
	modelsDevFile := fs.String("modelsdev-file", "", "optional local models.dev payload path")
	useFixture := fs.Bool("modelsdev-fixture", false, "use bundled models.dev fixture instead of live fetch")
	_ = fs.Parse(args)

	sources := catalog.DefaultBuildSources()
	if *anthropicFile != "" {
		for i := range sources {
			if _, ok := sources[i].Source.(catalog.AnthropicAPISource); ok {
				sources[i].Source = catalog.NewAnthropicAPISourceFromFile(*anthropicFile)
			}
		}
	}
	if *modelsDevFile != "" || *useFixture {
		for i := range sources {
			if _, ok := sources[i].Source.(catalog.ModelsDevSource); ok {
				cfg := catalog.NewModelsDevSource()
				cfg.FilePath = *modelsDevFile
				cfg.UseFixture = *useFixture
				sources[i].Source = cfg
			}
		}
	}
	builder := catalog.Builder{Sources: sources}
	built, err := builder.Build(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "build catalog: %v\n", err)
		os.Exit(1)
	}
	if err := catalog.SaveJSON(*outPath, built); err != nil {
		fmt.Fprintf(os.Stderr, "save catalog: %v\n", err)
		os.Exit(1)
	}
}

func validate(args []string) {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	path := fs.String("in", "catalog.json", "catalog JSON path")
	_ = fs.Parse(args)
	c, err := catalog.LoadJSON(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load catalog: %v\n", err)
		os.Exit(1)
	}
	if err := catalog.ValidateCatalog(c); err != nil {
		fmt.Fprintf(os.Stderr, "validate catalog: %v\n", err)
		os.Exit(1)
	}
}

func inspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	path := fs.String("in", "catalog.json", "catalog JSON path")
	serviceID := fs.String("service", "", "optional service ID to inspect")
	_ = fs.Parse(args)
	c, err := catalog.LoadJSON(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load catalog: %v\n", err)
		os.Exit(1)
	}
	if *serviceID == "" {
		for id := range c.Services {
			fmt.Println(id)
		}
		return
	}
	for _, offering := range c.OfferingsByService(*serviceID) {
		fmt.Printf("%s -> %s\n", offering.WireModelID, formatModelKey(offering.ModelKey))
	}
}

func modelShow(args []string) {
	fs := flag.NewFlagSet("model show", flag.ExitOnError)
	path := fs.String("in", "catalog.json", "catalog JSON path")
	name := fs.String("name", "", "logical model name (for example sonnet or claude-sonnet)")
	version := fs.String("version", "", "optional model version")
	_ = fs.Parse(args)

	c, err := catalog.LoadJSON(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load catalog: %v\n", err)
		os.Exit(1)
	}
	selector, err := catalog.ParseModelSelector(*name, *version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse selector: %v\n", err)
		os.Exit(1)
	}
	selection, err := c.SelectOfferingsByModel(selector)
	if err != nil {
		var ambiguous *catalog.AmbiguousModelSelectorError
		if errors.As(err, &ambiguous) {
			fmt.Fprintf(os.Stderr, "ambiguous model selector: name=%s version=%s\n\n", selector.Name, selector.Version)
			fmt.Fprintln(os.Stderr, "matches:")
			for _, model := range ambiguous.Candidates {
				fmt.Fprintf(os.Stderr, "- %s", formatModelKey(model.Key))
				if model.Name != "" {
					fmt.Fprintf(os.Stderr, "  %s", model.Name)
				}
				fmt.Fprintln(os.Stderr)
			}
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "select offerings: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("selector: name=%s", selection.Selector.Name)
	if selection.Selector.Version != "" {
		fmt.Printf(" version=%s", selection.Selector.Version)
	}
	if selection.Selector.ReleaseDate != "" {
		fmt.Printf(" release_date=%s", selection.Selector.ReleaseDate)
	}
	fmt.Println()
	fmt.Printf("model: %s\n", formatModelKey(selection.Model.Key))
	for _, item := range selection.Offerings {
		fmt.Printf("\n%s\n", item.Service.ID)
		fmt.Printf("  wire_model_id: %s\n", item.Offering.WireModelID)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: modeldb <build|validate|inspect|model> [flags]")
}

func modelUsage() {
	fmt.Fprintln(os.Stderr, "usage: modeldb model <show> [flags]")
}

func formatModelKey(key catalog.ModelKey) string {
	if releaseID := catalog.ReleaseID(key); releaseID != "" {
		return releaseID
	}
	return catalog.LineID(key)
}
