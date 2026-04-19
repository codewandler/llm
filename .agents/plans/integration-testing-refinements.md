# Integration Testing Refinements — Handover Document

This document is intended as a complete handover for the current state of the integration-testing cleanup work in `llm`, especially around reasoning / effort / reasoning-summary support after upgrading to recent `modeldb` releases. It should give enough context that another engineer can pick up the work without reading the prior chat log.

## 1. Main objective

The overall goal is to make the integration matrix truth come from `modeldb` exposure/capability data rather than repo-local heuristics or provider-specific hacks, and to leave the integration matrix/docs in a stable, commit-ready state.

Concretely, the goals were:
- stop collapsing `codex` into `openai` conceptually for testing/routing semantics
- model providers/services as distinct offerings over the same creator models
- move reasoning / effort / summary capability decisions toward `modeldb`
- reduce or remove local overlay quirks as `modeldb` becomes rich enough
- get matrix scenarios to reflect reality rather than skipping too aggressively
- regenerate `docs/integration-matrix.md` and `.json` from real runs

## 2. Important architectural conclusions reached during the work

### 2.1 Same model, different service/offering
A core point clarified during this work:
- `openai/gpt-5.4`
- `codex/gpt-5.4`
- `openrouter/openai/gpt-5.4`

may reference the same underlying OpenAI model family, but they are still different **service offerings**. The creator/model basis is OpenAI, but the service surface may still differ. This must be represented as:
- creator/basis truth
- plus offering-specific exposure/capability truth

rather than collapsing `codex -> openai` too early.

### 2.2 Reasoning support is not the same as visible reasoning summaries
A model/service may:
- consume reasoning tokens internally
- support effort controls
- but still not emit visible `reasoning_summary_*` events for every request/prompt

So the presence of reasoning tokens is not identical to “visible summary output was emitted”. This matters for the `thinking_text_comet` scenario.

### 2.3 Official OpenAI docs summary semantics
From docs research performed during this work, the important confirmed points are:
- `reasoning.summary` is the right parameter for requesting visible summaries
- official values are:
  - `auto`
  - `concise`
  - `detailed`
- summary support is model-dependent
- summaries are opt-in
- OpenAI docs recommend `auto` to obtain the most detailed summarizer available for a model
- `none` is an effort value, not an OpenAI summary value

Because of that, the preferred default in our provider transforms was reverted to:
- `summary = "auto"`
not `detailed`

### 2.4 Why fish mattered
The user’s Anthropic credentials are available in their fish environment, not necessarily in plain non-interactive bash. We successfully ran the matrix in fish using the same Go binary path that worked in bash. A note was added to `AGENTS.md` earlier describing the pattern.

## 3. Commits already made

Two commits were already created during this work:
- `b4da5f8` — `matrix: modeldb overlay for codex offerings`
- `dedf9f1` — `service: prefer exact offering service before basis fallback`

Do not redo those conceptually without checking current branch state.

## 4. Current working tree state at handoff time

At the time this handoff document was written, the working tree still had uncommitted changes in these files:
- `docs/integration-matrix.json`
- `docs/integration-matrix.md`
- `go.mod`
- `go.sum`
- `integration/providers.go`
- `integration/scenarios.go`
- `internal/modelcatalog/overlay.go`
- `internal/modelcatalog/overlay_test.go`
- `provider/openai/models.go`
- `provider/openai/openai.go`

No commit should be made automatically; the next engineer should inspect and decide what remains intentional.

## 5. What has already been changed in code

### 5.1 `modeldb` dependency
The repo was upgraded from older `modeldb` versions up to:
- `github.com/codewandler/modeldb v0.11.4`

This matters because older local overlay code was written against pre-`Exposures` shapes, while `v0.11.x` uses:
- `Offering.Exposures`
- `OfferingExposure.APIType`
- `OfferingExposure.ExposedCapabilities`
- `OfferingExposure.SupportedParameters`
- `OfferingExposure.ParameterValues`
- structured `Capabilities.Reasoning`

### 5.2 `internal/modelcatalog/overlay.go`
This file was intentionally simplified into a compatibility shim:
- `LoadMergedBuiltIn()` now just calls `LoadBuiltIn()`
- there is no additional llm-local merge step left there

Reason: by `modeldb v0.11.4`, the built-in published catalog already contains first-class Codex/OpenAI/OpenRouter exposure data needed for this work.

### 5.3 `internal/modelcatalog/overlay_test.go`
This file was rewritten to assert that built-in modeldb data already contains the expected enriched exposure data, for example:
- Codex `gpt-5.4` responses exposure contains reasoning, effort, summary support
- OpenAI `gpt-5.1` / `gpt-5.4` responses exposures contain reasoning metadata

### 5.4 `integration/providers.go`
This file was partially refactored toward `modeldb`-based capability lookup.

The intended direction now is:
- resolve exact `Offering` from the built-in catalog
- choose exact `Exposure(apiType)`
- derive support from `ExposedCapabilities.Reasoning`
- derive toggle support from either:
  - presence of `ReasoningModeOff`
  - or support for `reasoning_effort = none`

Explicit matrix targets now include:
- `openrouter_openai_gpt4o_mini`
- `openrouter_openai_gpt51`
- `openrouter_openai_gpt54`
- `openai_gpt4o`
- `openai_gpt51`
- `openai_gpt54`
- `codex_gpt54`
- plus the Anthropic / Claude / MiniMax targets

### 5.5 `provider/openai/openai.go`
This file was modified for two important reasons:

#### A. GPT-5.1 `ThinkingOff` bug fix
Previously, OpenAI GPT-5.1 with thinking off would map to wire `reasoning.effort = "none"`, but that value was incorrectly stuffed back into generic `llm.Effort`, which made request validation fail because `llm.Effort` does not allow `none`.

Current fix direction in the working tree:
- keep `llm.Request.Effort` valid
- carry the OpenAI-only wire value internally in request metadata
- reapply it only in the responses request transform
- strip the internal metadata key before the actual wire request is sent

This was necessary to make:
- `openai_gpt51/plain_text_pong`
- `system_prompt_kiwi`
- `thinking_off_respected`
work again.

#### B. reasoning summary default
The provider had briefly been changed to use `summary = "detailed"`, but after docs review and behavior review, it was reverted back to:
- `summary = "auto"`

### 5.6 `provider/openai/models.go`
This file still contains significant static model metadata.

It is still used for:
- routing policy (`useResponsesAPI`)
- effort/thinking policy (`mapEffortAndThinking`)
- provider alias policy (`ModelAliases`)
- fallback ordering / names (`modelRegistry`, `modelOrder`)

This file was not fully cleaned up yet. It needs a follow-up review to decide what remains justified now that `modeldb` has richer exposure/capability truth.

### 5.7 `integration/scenarios.go`
The reasoning scenario (`thinking_text_comet`) was strengthened from a weak / trivial task to a stronger math-style reasoning prompt because simple prompts were too brittle for visible reasoning summaries.

Current request shape is effectively:
- `ThinkingOn`
- `EffortHigh`
- stronger system prompt asking for reasoning summary if supported
- stronger user prompt requiring real multi-step arithmetic

This change was important: weak prompts produced many false negatives for visible summary output.

## 6. Modeldb version review summary

### 6.1 What `modeldb v0.11.4` now has
The latest published catalog now includes:

#### Codex
For `codex/gpt-5.4`:
- first-class `codex` service
- `openai-responses` exposure
- reasoning capability with:
  - efforts including `none`, `low`, `medium`, `high`, `xhigh`
  - summaries including `none`, `auto`, `concise`, `detailed`
  - modes including `enabled`, `off`
  - `visible_summary = true`
- parameter mappings for:
  - `reasoning.effort`
  - `reasoning.summary`

#### OpenAI
For `openai/gpt-5.1` and `openai/gpt-5.4`:
- `openai-responses` exposures exist
- reasoning capability exists
- `reasoning_effort` support is modeled
- `reasoning_summary` support is modeled
- GPT-5.1 now includes `none` in effort values

#### OpenRouter
For `openrouter/openai/gpt-5.1` and `openrouter/openai/gpt-5.4`:
- `openai-responses` exposure exists
- `openai-messages` exposure exists too
- `reasoning_effort` and `reasoning_summary` are now modeled in the responses exposure

This is a major improvement and is the reason local overlay hacks should now mostly disappear.

## 7. Docs and matrix runs already performed

### 7.1 Docs were regenerated multiple times
The following files were updated from real matrix runs:
- `docs/integration-matrix.md`
- `docs/integration-matrix.json`

Current checked-in working-tree docs (at handoff time) reflect a fish-shell run with the user’s real environment.

### 7.2 Focused reasoning comparison runs already done
Focused runs were performed across combinations like:
- `openai_gpt51`
- `openai_gpt54`
- `openrouter_openai_gpt51`
- `openrouter_openai_gpt54`
- `codex_gpt54`

At various points these runs showed:
- OpenAI direct can emit reasoning summary events
- OpenRouter OpenAI can emit reasoning summary events
- Codex can emit reasoning summary events in some runs
- some GPT-5.1 paths were unstable/flaky and sometimes returned only final text without visible reasoning summary

### 7.3 Important last focused result observed
The most recent focused comparison that mattered showed:
- `openai_gpt54/thinking_text_comet` → pass
- `openrouter_openai_gpt54/thinking_text_comet` → pass
- `codex_gpt54/thinking_text_comet` → pass
- `openai_gpt51` and especially `openrouter_openai_gpt51` still showed instability at different times

So GPT-5.1 remains the least stable path.

## 8. Remaining unresolved issues

### 8.1 `openai_gpt51` instability
Even after the `none` fix and stronger prompt, `openai_gpt51` was observed to be inconsistent across runs:
- sometimes visible reasoning summary events appear
- sometimes only final text appears

This does not currently look like an `agentapis` bug or a simple projection bug; it may be backend/model variability.

### 8.2 `openrouter_openai_gpt51` instability
OpenRouter GPT-5.1 also showed unstable/partial failures in some runs.

Now that `modeldb v0.11.4` has richer OpenRouter exposure data, the next step should be to verify whether remaining failures are:
- true runtime/provider behavior
- or a remaining repo-side mismatch in exact API exposure selection / assumptions

### 8.3 `provider/openai/models.go` cleanup unfinished
Static model defs remain there. They may still be partly needed for:
- routing policy
- provider alias policy

But capability truth should no longer live there conceptually; it should come from `modeldb` exposures.

## 9. Things that were explicitly ruled out during debugging

These were investigated and are unlikely to be the root cause of the remaining reasoning-summary issues:

- `agentapis` responses stream mapping itself
- llm-side event projection when raw `response.reasoning_summary_*` events are present
- simple “wrong summary field” diagnosis (`reasoning.summary` is correct)

When raw reasoning summary events exist, the current event pipeline surfaces them correctly.

## 10. Known user preferences / constraints

Very important for whoever continues this work:

1. The user strongly dislikes adding synthetic capability taxonomies or local quirks when the same truth should live in `modeldb`.
2. The user explicitly prefers:
   - creator/basis truth in modeldb
   - offering/service overlay truth in modeldb
   - repo-side code that simply queries that truth
3. Whenever a new quirk seems tempting, first consider whether this should instead be fixed in `modeldb`.
4. The user asked not to commit automatically at this stage; leave changes uncommitted until the state is truly ready.

## 11. Recommended next steps (ordered)

### Step 1 — inspect the current uncommitted diff carefully
Review exactly what remains in:
- `provider/openai/openai.go`
- `provider/openai/models.go`
- `integration/providers.go`
- `integration/scenarios.go`
- docs
- `go.mod` / `go.sum`

Make sure no stale experimental edits remain.

### Step 2 — keep the `modeldb v0.11.4` exposure-based path, remove old assumptions fully
Make sure the repo no longer depends on old offering fields or old overlay semantics in touched paths.

### Step 3 — investigate GPT-5.1 stability directly
Run focused tests repeatedly:

```bash
RUN_INTEGRATION=1 go test ./integration -tags integration -run 'TestIntegrationMatrix/(openai_gpt51|openrouter_openai_gpt51)/thinking_text_comet' -count=1 -v
```

Also do direct debug-style stream comparison for:
- `openai/gpt-5.1`
- `openrouter/openai/gpt-5.1`
- `openai/gpt-5.4`
- `openrouter/openai/gpt-5.4`
- `codex/gpt-5.4`

Compare:
- request body
- raw event names
- presence/absence of `response.reasoning_summary_*`
- usage reasoning tokens

### Step 4 — decide how to treat GPT-5.1 variability
Only after enough repeated runs:
- if stable enough, keep as pass
- if truly flaky, decide whether to:
  - strengthen prompt further
  - relax assertion carefully
  - or explicitly document backend instability

Do not casually invent local capability classes unless unavoidable.

### Step 5 — simplify `provider/openai/models.go`
Review what static model metadata is still truly required.
Likely keep only:
- routing policy if not yet safely derivable from modeldb exposure
- provider alias policy
- maybe fallback naming/order if catalog projection still needs it

Everything else should be reduced if modeldb now has the truth.

### Step 6 — rerun full matrix in fish and regenerate docs
Use fish with the known-good Go binary path if needed.
Then copy results to docs.

### Step 7 — only then prepare a final commit
Commit only once:
- diffs are intentional
- docs reflect final state
- no exploratory noise remains
- the remaining GPT-5.1 story is understood and accepted

## 12. Fish-shell integration test run recipe

The user’s real environment is best represented through fish. This exact pattern worked:

```bash
GOOD_GO=$(command -v go)
cat > /tmp/llm-matrix/run.fish <<EOF2
cd /home/timo/projects/llm
set -x RUN_INTEGRATION 1
set -x MATRIX_RESULTS_JSON /tmp/llm-matrix/results.json
set -x MATRIX_RESULTS_MD /tmp/llm-matrix/results.md
$GOOD_GO test ./integration -tags integration -run TestIntegrationMatrix -count=1 -v | tee /tmp/llm-matrix/test.out
EOF2
fish /tmp/llm-matrix/run.fish
```

If fish resolves a broken Go toolchain, keep using the bash-resolved absolute Go path.

## 13. Final reminder

The repo is close to a good state, but not yet safely commit-ready. The two main remaining tasks are:
- finish cleanup around `modeldb v0.11.4` so local quirks are minimized
- resolve or normalize the GPT-5.1 instability story

Do not lose sight of the user’s architectural goal: `modeldb` should carry the truth; `llm` should query it cleanly.
