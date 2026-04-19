package modelcatalog

import (
	"context"

	modeldb "github.com/codewandler/modeldb"
)

// LoadMergedBuiltIn remains as a compatibility shim for callers that still use
// the old overlay entry point. modeldb v0.11.x now carries the built-in
// OpenAI/Codex/OpenRouter exposure data we need, so there is no extra merge
// phase left in llm.
func LoadMergedBuiltIn() (modeldb.Catalog, error) {
	return LoadBuiltIn()
}

func ResolveMerged(ctx context.Context, sources ...modeldb.RegisteredSource) (modeldb.ResolvedCatalog, error) {
	base, err := LoadMergedBuiltIn()
	if err != nil {
		return modeldb.ResolvedCatalog{}, err
	}
	return ResolveWithBase(ctx, base, sources...)
}
