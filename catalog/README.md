# Catalog

`catalog` is the future standalone model graph for `codewandler/modeldb`.

It models:

- canonical model identity
- service/API offerings
- runtime-specific availability
- acquisition state for local runtimes
- additive overlays and provenance

At runtime the package loads a prebuilt `catalog.json` snapshot. Build-time tools
can refresh that snapshot from upstream sources such as `models.dev`, OpenAI, or
OpenRouter.

## Architecture

```text
                 +--------------------------+
                 | Build Sources            |
                 |--------------------------|
                 | Anthropic static         |
                 | OpenAI API               |
                 | OpenRouter API           |
                 | models.dev               |
                 | Bedrock curated          |
                 +------------+-------------+
                              |
                              v
                      +---------------+
                      | Builder       |
                      | merge/validate|
                      | emit snapshot |
                      +-------+-------+
                              |
                              v
                      +---------------+
                      | Catalog       |
                      |---------------|
                      | Models        |
                      | Services      |
                      | Offerings     |
                      +-------+-------+
                              |
              +---------------+----------------+
              |                                |
              v                                v
   +----------------------+         +------------------------+
   | Runtime Sources      |         | External Fragments     |
   |----------------------|         |------------------------|
   | Ollama               |         | private services       |
   | DockerMR             |         | private models         |
   | Bedrock runtime      |         | custom overlays        |
   +----------+-----------+         +-----------+------------+
              |                                 |
              +----------------+----------------+
                               |
                               v
                      +---------------+
                      | ResolvedCatalog|
                      |---------------|
                      | + runtimes     |
                      | + access       |
                      | + acquisition  |
                      +-------+-------+
                              |
                              v
                      +---------------+
                      | View          |
                      |---------------|
                      | filters       |
                      | sorting       |
                      | aliases       |
                      | preferences   |
                      +-------+-------+
                              |
                              v
               +----------------------------------+
               | Consumer Adapters                |
               |----------------------------------|
               | llm.Models / Resolve()           |
               | router aliases / selectors       |
               | autocomplete / favorites / UI    |
               +----------------------------------+
```

## Core entities

### ModelKey

The canonical identity key for a model.

- `Creator`
- `Family`
- `Series`
- `Version`
- `Variant`
- `ReleaseDate`

`ReleaseDate == ""` means a line-level identity or release-unknown record.

### ModelRecord

Canonical or provisional model metadata.

- factual aliases
- capabilities
- modalities
- pricing reference
- provenance
- enrichment fields like `OpenWeights`, `Attachment`, `LastUpdated`

### Service

An API/service surface that can expose models.

Examples:

- `anthropic`
- `openai`
- `openrouter`
- `bedrock`
- `ollama`
- `dockermr`

### Offering

A service-level mapping from a wire model ID to a canonical `ModelKey`.

### Runtime

A concrete runtime environment such as a local Ollama daemon or a specific
account/region context.

### RuntimeAccess

Whether an offering is routable right now for a specific runtime.

### RuntimeAcquisition

Whether an offering is known, installable, pullable, or otherwise acquirable.

## Merge rules

The graph is additive.

- new entities are allowed
- empty fields may be enriched
- mergeable collections are deduplicated and unioned
- conflicting non-empty scalar values are validation errors
- provenance is appended, never replaced

## Alias philosophy

The base catalog stores factual aliases only.

- creator aliases
- service aliases
- runtime-discovered aliases

Intent aliases such as `default`, `fast`, `powerful`, or user shortcuts do not
belong in the base graph. Those should be layered on top through view overlays
or consuming application policy.

## Views

Consumers should prefer `View` over direct map traversal.

Views are:

- service-scoped or runtime-scoped subsets of the graph
- filterable
- alias-indexed
- optionally preference-ranked

This is the primary API surface intended for consumers of the standalone module.

## Source layout

Not all source-related code belongs under `internal/source/...`.

- `catalog/source_*.go` contains public source adapters such as
  `NewModelsDevSource()` or `NewOllamaRuntimeSource()`
- `catalog/internal/source/...` contains upstream-specific implementation
  details such as transport helpers, fixtures, and wire schemas

This split is intentional: consumers of the future standalone module should be
able to construct source adapters directly, while upstream-specific internals
remain hidden.

## Snapshot generation

The embedded `catalog.json` snapshot is refreshed through the CLI.

From the `catalog/` directory:

```bash
go generate ./...
```

The current directive runs:

```bash
go run ./cmd/modeldb build --out catalog.json --modelsdev-file internal/source/modelsdev/testdata/api.json
```

That keeps runtime fast and deterministic while still allowing live refreshes
through the CLI when desired.

## Standalone module goal

This directory is being shaped so it can be extracted into a standalone module.

The key boundary rule is:

- `catalog` must not depend on the root repo's provider or `llm` packages

The root repo may adapt views from `catalog` into compatibility DTOs such as
`llm.Model`, but those adapters must live outside the package.
