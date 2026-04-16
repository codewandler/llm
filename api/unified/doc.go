// Package unified defines the canonical internal request and stream schemas plus
// three bridge layers:
//   - request bridges between the unified request model and protocol wire requests
//   - stream bridges between protocol-native events and the unified stream model
//   - forward bridges from unified stream events into the current llm publisher path
//
// The unified request model is semantic-first: canonical fields aim to express
// portable intent across protocols, while RequestExtras preserves native
// request details that are not part of the shared core.
//
// Projection is best-effort, not universally lossless. When a canonical request
// cannot be expressed safely on a target wire protocol, bridges prefer explicit
// projection errors over silently weakening semantics.
//
// Current protocol-specific limits:
//   - Anthropic Messages projects only RequestMetadata.EndUserID to the wire.
//   - Chat Completions only projects native content when a message is native-only.
//   - Responses outbound projection only supports one leading text-only system
//     message via instructions.
//   - Responses outbound assistant projection rejects non-projectable
//     text/tool interleaving and unsupported part kinds.
//   - Responses inbound conversion regroups contiguous assistant items into a
//     single assistant turn.
package unified
