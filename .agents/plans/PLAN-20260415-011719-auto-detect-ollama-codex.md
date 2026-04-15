# Plan: Auto-detect Ollama and Codex/ChatGPT in `provider/auto`

**Date**: 2026-04-15  
**Scope**: Add Ollama and ChatGPT/Codex providers to the `detectProviders` path in `provider/auto`.

---

## Context

`provider/auto` auto-detects providers by checking env vars or credential files. Two are missing:

- **Ollama** — no detection signal; no `WithOllama()` explicit option exists yet.
- **ChatGPT/Codex** — `WithCodexLocal()` exists as an opt-in option and
  `openai.CodexLocalAvailable()` does the file check, but `detectProviders` skips it.
  `TestDetectProviders_DoesNotAutoDetectCodexLocal` codified that opt-in behaviour — it must
  be replaced.

---

## Design

### Ollama — detected via `OLLAMA_HOST` env var only

Ollama has no API key and no credential file. `OLLAMA_HOST` is the explicit intent signal,
consistent with every other provider using an env var or credential file. No network probe.

Users running Ollama on the default port without `OLLAMA_HOST` use `WithOllama()` explicitly.

`detectProviders` signature is **unchanged** — no context threading needed.

### Codex/ChatGPT — detected via `~/.codex/auth.json`

`openai.CodexLocalAvailable()` is an existing pure file check. Wire it into `detectProviders`.

The factory must load `auth` eagerly (at detection time, not inside the factory closure), then
capture it — mirroring `WithCodexLocal()`. A factory that calls `openai.LoadCodexAuth()` inside
the closure and falls back to `openai.New()` on error would silently register a wrong provider.

### Double-registration

When a user calls `WithCodexLocal()` explicitly and `~/.codex/auth.json` also exists,
auto-detection fires a second ChatGPT entry. The deduplication logic in `auto.go` renames it
`chatgpt-2`. The same situation already exists for `WithBedrock()` + AWS credentials. The fix
is: call `WithoutChatGPT()` alongside `WithCodexLocal()` if two instances are unwanted. This is
documented in `WithoutChatGPT()`'s godoc. No code change is needed.

The identical situation applies to `WithOllama()` + `OLLAMA_HOST` being set.

### Factory option ordering — follow OpenAI pattern

Every factory in `detect.go` appends infrastructure options (httpClient) **after** `opts`,
so they win over any conflicting value in `llmOpts`. For Ollama, both `baseURL` and `httpClient`
must be appended after `opts`. Use `opts = append(opts, ...)` (same idiom as OpenAI's factory)
rather than building a separate `ollamaOpts` slice.

### `Available()` and `BaseURL()` live in a new file

The established pattern: `claude.LocalTokenProviderAvailable()` is in `claude/local.go`,
`openai.CodexLocalAvailable()` is in `openai/codex.go`. Availability concerns go in their own
file. Create `provider/ollama/available.go` and `provider/ollama/available_test.go`.

### `modelAliasesForProvider` — no change needed

The existing `default: return nil` already covers `ProviderOllama`. Only a test assertion is
added to verify the default branch.

### Ollama `hasAliases: false`

Ollama's model list is installation-dependent. It does not contribute to the
`fast/default/powerful/codex` global aliases.

---

## Files changed

| File | Change |
|------|--------|
| `provider/ollama/available.go` | **New** — `EnvOllamaHost`, `Available()`, `BaseURL()` |
| `provider/ollama/available_test.go` | **New** — `TestAvailable_*`, `TestBaseURL_*` |
| `provider/auto/constants.go` | Add `ProviderOllama` |
| `provider/auto/detect.go` | Add Ollama + Codex detection blocks |
| `provider/auto/auto.go` | Update `New` docstring |
| `provider/auto/options.go` | Add `WithOllama()`, `WithoutOllama()`, `WithoutChatGPT()` |
| `provider/auto/auto_test.go` | Replace Codex test; add Ollama + Codex tests; extend `TestConstants` and `TestModelAliasesForProvider` |

---

## Tasks

---

### Task 1: Create `provider/ollama/available.go`

**Files created**: `provider/ollama/available.go`  
**Estimated time**: 2 minutes

`defaultBaseURL` is already defined in `ollama.go` in the same package, so it is accessible here.

```go
package ollama

import "os"

// EnvOllamaHost is the environment variable that overrides the Ollama base URL.
// Set it to signal that Ollama is available for auto-detection:
//
//	export OLLAMA_HOST=http://localhost:11434
const EnvOllamaHost = "OLLAMA_HOST"

// Available reports whether Ollama is configured in the environment.
// It returns true when OLLAMA_HOST is set to a non-empty value.
//
// To use Ollama on the default port without setting OLLAMA_HOST,
// add it explicitly with auto.WithOllama().
func Available() bool {
	return os.Getenv(EnvOllamaHost) != ""
}

// BaseURL returns the effective Ollama base URL.
// Returns the value of OLLAMA_HOST if set, otherwise http://localhost:11434.
func BaseURL() string {
	if h := os.Getenv(EnvOllamaHost); h != "" {
		return h
	}
	return defaultBaseURL
}
```

**Verification**:
```bash
go build ./provider/ollama/...
```

---

### Task 2: Create `provider/ollama/available_test.go`

**Files created**: `provider/ollama/available_test.go`  
**Estimated time**: 2 minutes

```go
package ollama

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAvailable_WithEnvVar(t *testing.T) {
	t.Setenv(EnvOllamaHost, "http://localhost:11434")
	assert.True(t, Available())
}

func TestAvailable_WithoutEnvVar(t *testing.T) {
	t.Setenv(EnvOllamaHost, "")
	assert.False(t, Available())
}

func TestBaseURL_Default(t *testing.T) {
	t.Setenv(EnvOllamaHost, "")
	assert.Equal(t, "http://localhost:11434", BaseURL())
}

func TestBaseURL_FromEnv(t *testing.T) {
	t.Setenv(EnvOllamaHost, "http://remote:11434")
	assert.Equal(t, "http://remote:11434", BaseURL())
}
```

All four tests are deterministic — no network calls.

**Verification**:
```bash
go test ./provider/ollama/... -run "TestAvailable|TestBaseURL" -v
```

---

### Task 3: Add `ProviderOllama` to `provider/auto/constants.go`

**Files modified**: `provider/auto/constants.go`  
**Estimated time**: 1 minute

Add to the `Provider type names` const block:

```go
ProviderOllama = "ollama"
```

**Verification**:
```bash
go build ./provider/auto/...
```

---

### Task 4: Add Ollama and Codex detection to `provider/auto/detect.go`

**Files modified**: `provider/auto/detect.go`  
**Estimated time**: 5 minutes

Function signature is **unchanged**.

Add to the import block:
```go
"github.com/codewandler/llm/provider/ollama"
```

(`openai`, `net/http`, and `os` are already imported.)

Append at the end of `detectProviders`, before `return providers`. Use `opts = append(opts, ...)`
to match the OpenAI factory pattern — infrastructure options appended last so they win.

```go
// 7. Ollama — detected when OLLAMA_HOST is set.
if !disabled[ProviderOllama] && ollama.Available() {
	baseURL := ollama.BaseURL()
	providers = append(providers, providerEntry{
		name:         ProviderOllama,
		providerType: ProviderOllama,
		factory: func(opts ...llm.Option) llm.Provider {
			opts = append(opts, llm.WithBaseURL(baseURL))
			if httpClient != nil {
				opts = append(opts, llm.WithHTTPClient(httpClient))
			}
			return ollama.New(opts...)
		},
		modelAliases: nil,
		hasAliases:   false,
	})
}

// 8. ChatGPT/Codex — detected when ~/.codex/auth.json is present and readable.
// auth is loaded eagerly so the factory closure captures a valid *CodexAuth,
// mirroring the WithCodexLocal() pattern.
if !disabled[ProviderChatGPT] && openai.CodexLocalAvailable() {
	if auth, err := openai.LoadCodexAuth(); err == nil {
		providers = append(providers, providerEntry{
			name:         ProviderChatGPT,
			providerType: ProviderChatGPT,
			factory: func(opts ...llm.Option) llm.Provider {
				var base http.RoundTripper
				if httpClient != nil {
					base = httpClient.Transport
				}
				return auth.NewProvider(base)
			},
			modelAliases: openai.CodexModelAliases,
			hasAliases:   true,
		})
	}
}
```

**Verification**:
```bash
go build ./provider/auto/...
go vet ./provider/auto/...
```

---

### Task 5: Update the `New` docstring in `provider/auto/auto.go`

**Files modified**: `provider/auto/auto.go`  
**Estimated time**: 2 minutes

The existing list stops at OpenRouter (MiniMax is already detected but was never listed). Replace
the entire auto-detect section and update the explicit-options example to include the new options:

```go
// New creates an aggregate provider with auto-detected or explicitly configured providers.
//
// Without options, it auto-detects available providers in priority order:
//   - Claude local (~/.claude credentials)
//   - Anthropic direct API (if ANTHROPIC_API_KEY is set)
//   - AWS Bedrock (if AWS_ACCESS_KEY_ID, AWS_PROFILE, or container credentials are set)
//   - OpenAI (if OPENAI_API_KEY or OPENAI_KEY is set)
//   - OpenRouter (if OPENROUTER_API_KEY is set)
//   - MiniMax (if MINIMAX_API_KEY is set)
//   - Ollama (if OLLAMA_HOST is set)
//   - ChatGPT/Codex (if ~/.codex/auth.json is present)
//
// With explicit options, you can configure specific providers:
//
//	auto.New(ctx,
//	    auto.WithName("myapp"),
//	    auto.WithClaude(tokenStore),  // Claude accounts from store
//	    auto.WithClaudeLocal(),       // Claude local credentials
//	    auto.WithBedrock(),           // AWS Bedrock
//	    auto.WithOllama(),            // Ollama (default or OLLAMA_HOST port)
//	    auto.WithCodexLocal(),        // ChatGPT/Codex via ~/.codex/auth.json
//	)
```

**Verification**:
```bash
go build ./provider/auto/...
```

---

### Task 6: Add `WithOllama()`, `WithoutOllama()`, `WithoutChatGPT()` to `provider/auto/options.go`

**Files modified**: `provider/auto/options.go`  
**Estimated time**: 3 minutes

Add `"github.com/codewandler/llm/provider/ollama"` to the import block, then append:

```go
// WithOllama adds the Ollama local provider.
// The base URL is read from OLLAMA_HOST if set, otherwise http://localhost:11434.
// Use this to include Ollama explicitly when OLLAMA_HOST is not set but Ollama
// is running on the default port.
//
// If OLLAMA_HOST is set and auto-detection is active, calling WithOllama() will
// register a second Ollama instance alongside the auto-detected one. Pass
// WithoutAutoDetect() or WithoutOllama() to avoid duplication.
func WithOllama() Option {
	return func(c *config) {
		baseURL := ollama.BaseURL()
		httpClient := c.httpClient
		c.providers = append(c.providers, providerEntry{
			name:         ProviderOllama,
			providerType: ProviderOllama,
			factory: func(opts ...llm.Option) llm.Provider {
				opts = append(opts, llm.WithBaseURL(baseURL))
				if httpClient != nil {
					opts = append(opts, llm.WithHTTPClient(httpClient))
				}
				return ollama.New(opts...)
			},
			modelAliases: nil,
			hasAliases:   false,
		})
	}
}

// WithoutOllama is a convenience shorthand for WithoutProvider(ProviderOllama).
// It prevents Ollama from being auto-detected even when OLLAMA_HOST is set.
func WithoutOllama() Option {
	return WithoutProvider(ProviderOllama)
}

// WithoutChatGPT is a convenience shorthand for WithoutProvider(ProviderChatGPT).
// It prevents ChatGPT/Codex from being auto-detected even when ~/.codex/auth.json
// is present. Useful when calling WithCodexLocal() explicitly to avoid registering
// a duplicate chatgpt-2 instance alongside the auto-detected one.
func WithoutChatGPT() Option {
	return WithoutProvider(ProviderChatGPT)
}
```

**Verification**:
```bash
go build ./provider/auto/...
```

---

### Task 7: Update `provider/auto/auto_test.go`

**Files modified**: `provider/auto/auto_test.go`  
**Estimated time**: 5 minutes

#### 7a. Add import

```go
"github.com/codewandler/llm/provider/ollama"
```

#### 7b. Replace `TestDetectProviders_DoesNotAutoDetectCodexLocal`

Delete the existing test and add two replacements. Use the same `auth.json` payload as the
original test (`"access_token":"synthetic-access-token"`).

```go
func TestDetectProviders_CodexLocalDetected(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(home, ".codex", "auth.json"),
		[]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-access-token","account_id":"test-account"}}`),
		0o600,
	))
	t.Setenv("HOME", home)
	t.Setenv(ollama.EnvOllamaHost, "") // prevent Ollama from firing

	providers := detectProviders(nil, nil, map[string]bool{
		ProviderClaude:     true,
		ProviderAnthropic:  true,
		ProviderBedrock:    true,
		ProviderOpenAI:     true,
		ProviderOpenRouter: true,
		ProviderMiniMax:    true,
		ProviderOllama:     true,
	})

	require.Len(t, providers, 1)
	assert.Equal(t, ProviderChatGPT, providers[0].name)
	assert.Equal(t, ProviderChatGPT, providers[0].providerType)
}

func TestDetectProviders_CodexLocalNotDetected_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // empty — no .codex/auth.json
	t.Setenv(ollama.EnvOllamaHost, "")

	providers := detectProviders(nil, nil, map[string]bool{
		ProviderClaude:     true,
		ProviderAnthropic:  true,
		ProviderBedrock:    true,
		ProviderOpenAI:     true,
		ProviderOpenRouter: true,
		ProviderMiniMax:    true,
		ProviderOllama:     true,
	})

	require.Empty(t, providers)
}
```

#### 7c. Add Ollama detection tests

```go
func TestDetectProviders_OllamaDetected_EnvVar(t *testing.T) {
	t.Setenv(ollama.EnvOllamaHost, "http://localhost:11434")
	t.Setenv("HOME", t.TempDir()) // no .codex/auth.json

	providers := detectProviders(nil, nil, map[string]bool{
		ProviderClaude:     true,
		ProviderAnthropic:  true,
		ProviderBedrock:    true,
		ProviderOpenAI:     true,
		ProviderOpenRouter: true,
		ProviderMiniMax:    true,
		ProviderChatGPT:    true,
	})

	require.Len(t, providers, 1)
	assert.Equal(t, ProviderOllama, providers[0].name)
	assert.Equal(t, ProviderOllama, providers[0].providerType)
}

func TestDetectProviders_OllamaNotDetected_NoEnvVar(t *testing.T) {
	t.Setenv(ollama.EnvOllamaHost, "")
	t.Setenv("HOME", t.TempDir())

	providers := detectProviders(nil, nil, map[string]bool{
		ProviderClaude:     true,
		ProviderAnthropic:  true,
		ProviderBedrock:    true,
		ProviderOpenAI:     true,
		ProviderOpenRouter: true,
		ProviderMiniMax:    true,
		ProviderChatGPT:    true,
	})

	require.Empty(t, providers)
}
```

#### 7d. Extend `TestConstants`

```go
assert.NotEmpty(t, ProviderOllama)
```

#### 7e. Extend `TestModelAliasesForProvider`

`ProviderOllama` hits the existing `default: return nil` branch. Add one assertion to cover it:

```go
ollamaAliases := modelAliasesForProvider(ProviderOllama)
assert.Nil(t, ollamaAliases, "ollama has no shorthand aliases")
```

**Verification**:
```bash
go test ./provider/auto/... -v -run "TestDetectProviders|TestConstants|TestModelAliases"
```

---

## Final verification

```bash
go build ./...
go vet ./...
go test ./provider/ollama/... -v
go test ./provider/auto/... -v
```

---

## Total estimated time: ~20 minutes
