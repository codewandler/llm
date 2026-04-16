package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/codewandler/llm/catalog"
)

func main() {
	outPath := flag.String("out", "catalog.json", "output catalog JSON path")
	flag.Parse()

	builder := catalog.Builder{
		Sources: []catalog.RegisteredSource{
			{
				Stage:     catalog.StageBuild,
				Authority: catalog.AuthorityCanonical,
				Source:    catalog.NewAnthropicStaticSource(),
			},
			{
				Stage:     catalog.StageBuild,
				Authority: catalog.AuthorityEnrichment,
				Source:    catalog.NewModelDBSource(),
			},
		},
	}
	if src := catalog.NewOpenAISourceFromEnv(); src.APIKey != "" {
		builder.Sources = append(builder.Sources, catalog.RegisteredSource{
			Stage:     catalog.StageBuild,
			Authority: catalog.AuthorityTrusted,
			Source:    src,
		})
	}
	if src := catalog.NewOpenRouterSourceFromEnv(); src.APIKey != "" {
		builder.Sources = append(builder.Sources, catalog.RegisteredSource{
			Stage:     catalog.StageBuild,
			Authority: catalog.AuthorityTrusted,
			Source:    src,
		})
	}

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
