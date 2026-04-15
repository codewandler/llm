# `api/responses`

Wire-level client, types, constants, and streaming parser for the OpenAI Responses API.

## Purpose

This package models the native OpenAI Responses wire protocol:

- request and response JSON shapes
- documented streaming event names
- typed native event parsing

It is intentionally a protocol package, not a normalized abstraction layer.

## Scope

`api/responses` is used against more than just OpenAI itself. It is also useful for providers such as OpenRouter when they expose the same, or a closely compatible, OpenAI Responses API surface.

That does not change the package's job:

- this package preserves native wire details
- higher layers decide what is portable or worth normalizing

Cross-provider normalization belongs in `api/unified`, not here.

## Source Of Truth

When modeling exact event names, payloads, and terminal semantics in this package, the source-of-truth order is:

1. OpenAI Responses API documentation and observed native wire behavior
2. Open Responses as a semantic and interoperability reference

This means `api/responses` prefers exact OpenAI-native event identity over reducing everything to a portable subset.

## Relationship To Open Responses

The [Open Responses specification](https://www.openresponses.org/specification) is still relevant here.

It is useful for:

- lifecycle and state-machine semantics
- streaming ordering expectations
- extension philosophy for provider-specific items and events
- thinking about what should eventually be portable across providers

But this package does not treat Open Responses as the primary authority for exact wire modeling. OpenAI-native item and event names win whenever we need to choose between:

- exact native protocol fidelity
- a more general cross-provider abstraction

That matters because providers that are OpenAI-compatible in practice often follow the OpenAI wire format directly, even when that format does not exactly mirror Open Responses naming or extension conventions.

## Parsing Policy

The parser in this package follows these rules:

- parse all documented OpenAI Responses streaming events
- preserve native event identity at the `api/responses` layer
- allow unknown future events to pass through as no-ops instead of guessing

This package is therefore suitable for consumers that want to inspect or log native wire events exactly as they appear on the stream.

## Compatibility Policy

Non-OpenAI providers can use this package when they speak the OpenAI Responses wire protocol closely enough that native parsing still applies.

This package does not intentionally preserve non-spec quirks or alternative event names. If a provider diverges from the native OpenAI surface, that divergence should be handled outside this package or by a separate, explicit protocol layer.

## Package Docs

This directory includes two human-maintained package docs:

- `README.md` for package intent, scope, and compatibility policy
- `api.md` for the detailed request and streaming-event reference tied to this package's types

These files are curated references for the package. The live OpenAI docs remain authoritative.

## Non-Goals

This package does not:

- define the canonical cross-provider event model
- decide which native events should be surfaced through `llm`
- force all providers into the Open Responses portable subset

Those decisions belong in `api/unified` and the provider integration layers.
