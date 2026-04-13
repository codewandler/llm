# Thinking and Effort

Two fields on `llm.Request` control how the model reasons and how hard it works:

- **`Effort`** — how thoroughly the model works (universal across all providers)
- **`Thinking`** — whether extended/chain-of-thought reasoning is used (mode toggle)

These are orthogonal: you can set effort without thinking, thinking without effort,
both, or neither.

## Effort

Controls thinking depth, response length, and tool call count.

| Value | Description |
|---|---|
| `""` (unspecified) | Provider picks its default |
| `"low"` | Fast, cheap, less thorough |
| `"medium"` | Balanced |
| `"high"` | Thorough, slower |
| `"max"` | Maximum capability. Silently downgrades to `"high"` on models that don't support it |

## Thinking

Controls whether the model uses extended thinking. This is a mode selector, not a
depth control — depth is controlled by `Effort`.

| Value | Description |
|---|---|
| `""` (auto) | Provider/model decides whether to think |
| `"on"` | Force extended thinking on |
| `"off"` | Force extended thinking off |

## Defaults

| Entry point | Effort | Thinking | Rationale |
|---|---|---|---|
| CLI (no flags) | unspecified | auto | Provider decides everything |
| `newDefaultRequest()` | `low` | `off` | Conservative programmatic default |
| `Coding()` preset | `high` | `on` | Coding benefits from thorough reasoning |

## Provider Behavior

### Anthropic ≥ 4.6 (Sonnet 4.6, Opus 4.6)

| Thinking | Effort | API params |
|---|---|---|
| auto | unspecified | `thinking: {type: "adaptive"}`, `output_config: {effort: "medium"}` |
| auto | high | `thinking: {type: "adaptive"}`, `output_config: {effort: "high"}` |
| auto | max | `thinking: {type: "adaptive"}`, `output_config: {effort: "max"}` |
| off | any | `thinking: {type: "disabled"}`, `output_config: {effort: <effort>}` |
| on | any | `thinking: {type: "adaptive"}`, `output_config: {effort: <effort>}` |

- `auto` and `on` both produce adaptive thinking (adaptive IS the "on" mode for 4.6)
- `budget_tokens` is never sent — depth is controlled exclusively via `output_config.effort`
- `output_config.effort` is set regardless of thinking mode

### Anthropic < 4.6 (Haiku 4.5, Sonnet 4.5, Opus 4.5)

| Thinking | Effort | API params |
|---|---|---|
| auto | unspecified | `thinking: {type: "enabled", budget_tokens: 31999}` |
| auto | low | `thinking: {type: "enabled", budget_tokens: 1024}` |
| auto | medium | `thinking: {type: "enabled", budget_tokens: ~16512}` |
| auto | high | `thinking: {type: "enabled", budget_tokens: 31999}` |
| off | any | `thinking: {type: "disabled"}` |
| on | any | `thinking: {type: "enabled", budget_tokens: <from Effort>}` |
| any | max | silent downgrade to `high` |

- Effort maps to `budget_tokens` via `Effort.ToBudget(1024, 31999)`
- `output_config.effort` is also set on Opus 4.5 (which supports it)
- Haiku 4.5 and Sonnet 4.5 don't support `output_config.effort`

### OpenAI

| Thinking | Effort | Model category | API params |
|---|---|---|---|
| auto | unspecified | any | omit `reasoning_effort` |
| auto | high | any | `reasoning_effort: "high"` |
| off | any | GPT-5.1+ | `reasoning_effort: "none"` |
| off | any | pre-GPT-5.1 / Codex / Pro | omit (can't reliably disable) |
| off | any | non-reasoning | no-op |
| on | unspecified | reasoning | `reasoning_effort: "high"` |
| on | any | non-reasoning | no-op |
| any | max | Codex | `reasoning_effort: "xhigh"` |
| any | max | other | downgrade to `"high"` |

- Effort values (`low`, `medium`, `high`) map 1:1 to `reasoning_effort`
- `ThinkingOff` is gracefully ignored on models that don't support it
- `ThinkingOn` on non-reasoning models is a no-op

### Bedrock

| Thinking | Effort | API params |
|---|---|---|
| auto/on | unspecified | `reasoning_config: {type: "enabled", budget_tokens: 31999}` |
| auto/on | high | `reasoning_config: {type: "enabled", budget_tokens: 31999}` |
| auto/on | low | `reasoning_config: {type: "enabled", budget_tokens: 1024}` |
| off | any | omit `reasoning_config` |
| any | max | silent downgrade to `high` |

- Same pattern as Anthropic < 4.6 (Bedrock uses the Anthropic wire format)

### OpenRouter

| Thinking | Effort | API params |
|---|---|---|
| auto | unspecified | omit `reasoning` |
| auto | high | `reasoning: {effort: "high"}` |
| on | unspecified | `reasoning: {effort: "high"}` |
| off | any | omit `reasoning` |
| any | max | downgrade to `"high"` |

- Uses the OpenAI Chat Completions wire format

## CLI Usage

```bash
# Default: provider decides everything
llmcli infer "Hello"

# Control effort
llmcli infer --effort high "Explain Go channels"
llmcli infer --effort max "Write a compiler"

# Control thinking mode
llmcli infer --thinking off "Quick answer please"
llmcli infer --thinking on --effort high "Complex problem"

# Verbose: see what params are actually sent
llmcli infer -v --effort high --thinking on "Explain quantum computing"
```

## Migration from Old API

| Old flag / field | New flag / field |
|---|---|
| `--thinking none` | `--thinking off` |
| `--thinking low` | `--effort low` |
| `--thinking medium` | `--effort medium` |
| `--thinking high` | `--effort high` |
| `--thinking xhigh` | `--effort max` |
| `--effort low` | `--effort low` (unchanged) |
| `--effort high` | `--effort high` (unchanged) |
| `ThinkingEffort` field | `Effort` + `Thinking` fields |
| `OutputEffort` field | `Effort` field |
