package auto

import (
	"net/http"

	"github.com/codewandler/llm"
	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	providerregistry "github.com/codewandler/llm/internal/providerregistry"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/codex"
	"github.com/codewandler/llm/provider/dockermr"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

type claudeStoreEntry struct{ store claude.TokenStore }

type config struct {
	name              string
	providers         []llm.RegisteredProvider
	detectedProviders []llm.DetectedProvider
	claudeStores      []claudeStoreEntry
	autoDetect        bool
	builtinAliases    bool
	disabledTypes     map[string]bool
	globalAliases     map[string][]string
	httpClient        *http.Client
	llmOpts           []llm.Option
}

type Option func(*config)

func WithHTTPClient(client *http.Client) Option { return func(c *config) { c.httpClient = client } }
func WithLLMOptions(opts ...llm.Option) Option {
	return func(c *config) { c.llmOpts = append(c.llmOpts, opts...) }
}
func WithName(name string) Option   { return func(c *config) { c.name = name } }
func WithoutAutoDetect() Option     { return func(c *config) { c.autoDetect = false } }
func WithoutBuiltinAliases() Option { return func(c *config) { c.builtinAliases = false } }
func WithoutProvider(providerType string) Option {
	return func(c *config) {
		if c.disabledTypes == nil {
			c.disabledTypes = make(map[string]bool)
		}
		c.disabledTypes[providerType] = true
	}
}
func WithoutBedrock() Option  { return WithoutProvider(ProviderBedrock) }
func WithoutOllama() Option   { return WithoutProvider(ProviderOllama) }
func WithoutCodex() Option    { return WithoutProvider(ProviderCodex) }
func WithoutDockerMR() Option { return WithoutProvider(ProviderDockerMR) }

func WithClaude(store claude.TokenStore) Option {
	return func(c *config) { c.claudeStores = append(c.claudeStores, claudeStoreEntry{store: store}) }
}

func WithClaudeAccount(name string, store claude.TokenStore) Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: name, Type: ProviderClaude, Params: map[string]any{"store": store, "accountKey": name}})
	}
}

func WithClaudeLocal() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderClaude, Type: ProviderClaude})
	}
}
func WithCodexLocal() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderCodex, Type: ProviderCodex})
	}
}
func WithBedrock() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderBedrock, Type: ProviderBedrock})
	}
}
func WithOpenAI() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderOpenAI, Type: ProviderOpenAI})
	}
}
func WithOpenRouter() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderOpenRouter, Type: ProviderOpenRouter})
	}
}
func WithAnthropic() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderAnthropic, Type: ProviderAnthropic})
	}
}
func WithOllama() Option {
	return func(c *config) {
		c.detectedProviders = append(c.detectedProviders, llm.DetectedProvider{Name: ProviderOllama, Type: ProviderOllama, Params: map[string]any{"baseURL": ollama.BaseURL()}})
	}
}
func WithDockerModelRunner(opts ...llm.Option) Option {
	return func(c *config) {
		c.providers = append(c.providers, llm.RegisteredProvider{ServiceID: ProviderDockerMR, Provider: dockermr.New(opts...)})
	}
}

func WithGlobalAlias(alias string, targets ...string) Option {
	return func(c *config) {
		if c.globalAliases == nil {
			c.globalAliases = make(map[string][]string)
		}
		c.globalAliases[alias] = targets
	}
}
func WithGlobalAliases(aliases map[string][]string) Option {
	return func(c *config) {
		if c.globalAliases == nil {
			c.globalAliases = make(map[string][]string)
		}
		for alias, targets := range aliases {
			c.globalAliases[alias] = targets
		}
	}
}

func builtinIntentAliases() map[string]llm.IntentSelector {
	selectors := map[string]llm.IntentSelector{}
	if cat, err := modelcatalog.LoadBuiltIn(); err == nil {
		for alias, service := range map[string]string{AliasFast: ProviderOpenAI, AliasDefault: ProviderOpenAI, AliasPowerful: ProviderOpenAI} {
			if models := providerregistryModels(cat, service); len(models) > 0 {
				selectors[alias] = llm.IntentSelector{Model: service + "/" + models[0].ID}
			}
		}
	}
	if _, ok := selectors[AliasFast]; !ok {
		selectors[AliasFast] = llm.IntentSelector{Model: ProviderOpenAI + "/" + openai.ModelGPT4oMini}
	}
	if _, ok := selectors[AliasDefault]; !ok {
		selectors[AliasDefault] = llm.IntentSelector{Model: ProviderAnthropic + "/" + anthropic.ModelSonnet}
	}
	if _, ok := selectors[AliasPowerful]; !ok {
		selectors[AliasPowerful] = llm.IntentSelector{Model: ProviderOpenAI + "/" + openai.ModelO3}
	}
	_ = bedrock.ModelSonnetLatest
	_ = codex.BuiltinAliasModels
	return selectors
}

func providerregistryModels(cat modelcatalog.Snapshot, service string) llm.Models {
	reg := providerregistry.New()
	_ = reg
	// use actual provider package model catalogs as fallback-friendly source
	switch service {
	case ProviderOpenAI:
		return openai.New().Models()
	case ProviderAnthropic:
		return anthropic.New().Models()
	case ProviderOpenRouter:
		return openrouter.New().Models()
	default:
		_ = cat
		return nil
	}
}
