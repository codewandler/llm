# Changelog

## Unreleased

### Changed

- Builder snapshot generation is now creator-first.
- Creator/direct sources define root `ModelRecord`s.
- Broker/platform sources such as OpenRouter and `models.dev` now rebind onto
  creator-owned roots when a matching creator model line already exists.
- Broker/platform fragments still create provisional non-canonical roots when no
  creator root exists, preserving fallback coverage.

### Fixed

- Cross-service offerings no longer leave duplicate root models behind when a
  creator source already provides the canonical model.
- `modeldb model show --name sonnet --version 4.5` now resolves cleanly to the
  Anthropic canonical Claude Sonnet 4.5 release instead of surfacing a
  line-vs-release ambiguity caused by broker-derived root records.

### Model Changes

- `catalog.json` was regenerated with creator-root rebinding enabled.
- Root model count dropped from `583` to `526` because duplicate broker-derived
  roots were collapsed onto creator-owned canonical roots.
- Anthropic-backed offerings across `anthropic`, `bedrock`, and `openrouter`
  now consistently attach to the same creator root for a given Claude model line.
- A large set of duplicate OpenAI release/enrichment roots were removed, with
  metadata merged into the surviving creator-owned roots.
- The regenerated snapshot also picked up normal upstream drift from live source
  refreshes, including changes in pricing, limits, supported parameters, and
  metadata such as `knowledge_cutoff` and `last_updated`.
