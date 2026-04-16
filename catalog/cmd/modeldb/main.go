package main

import (
	"context"
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
	default:
		usage()
		os.Exit(2)
	}
}

func build(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outPath := fs.String("out", "catalog.json", "output catalog JSON path")
	modelsDevFile := fs.String("modelsdev-file", "", "optional local models.dev payload path")
	useFixture := fs.Bool("modelsdev-fixture", false, "use bundled models.dev fixture instead of live fetch")
	_ = fs.Parse(args)

	sources := catalog.DefaultBuildSources()
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
		fmt.Printf("%s -> %s\n", offering.WireModelID, offering.ModelKey)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: modeldb <build|validate|inspect> [flags]")
}
