package providerregistry

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/codex"
	"github.com/codewandler/llm/provider/dockermr"
	"github.com/codewandler/llm/provider/minimax"
	"github.com/codewandler/llm/provider/ollama"
	"github.com/codewandler/llm/provider/openai"
	"github.com/codewandler/llm/provider/openrouter"
)

type Registry struct{ defs map[string]Definition }

type Definition struct {
	Type   string
	Detect func(context.Context, DetectEnv) ([]llm.DetectedProvider, error)
	Build  func(context.Context, BuildConfig) (llm.Provider, error)
}

type DetectEnv struct {
	HTTPClient *http.Client
	LLMOptions []llm.Option
}

type BuildConfig struct {
	Name       string
	Type       string
	Params     map[string]any
	HTTPClient *http.Client
	LLMOptions []llm.Option
}

func New() *Registry {
	r := &Registry{defs: map[string]Definition{}}
	registerDefaults(r)
	return r
}

func (r *Registry) Register(def Definition) { r.defs[def.Type] = def }
func (r *Registry) Definition(typeName string) (Definition, bool) {
	d, ok := r.defs[typeName]
	return d, ok
}

func (r *Registry) Detect(ctx context.Context, env llm.DetectEnv, disabled map[string]bool) ([]llm.DetectedProvider, error) {
	var out []llm.DetectedProvider
	for _, typeName := range orderedTypes() {
		if disabled[typeName] {
			continue
		}
		def, ok := r.defs[typeName]
		if !ok || def.Detect == nil {
			continue
		}
		items, err := def.Detect(ctx, DetectEnv{HTTPClient: env.HTTPClient, LLMOptions: env.LLMOptions})
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

func (r *Registry) Build(ctx context.Context, req llm.DetectedProvider, client *http.Client, opts []llm.Option) (llm.Provider, error) {
	def, ok := r.defs[req.Type]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", req.Type)
	}
	return def.Build(ctx, BuildConfig{Name: req.Name, Type: req.Type, Params: req.Params, HTTPClient: client, LLMOptions: opts})
}

func orderedTypes() []string {
	return []string{"claude", "anthropic", "bedrock", "openai", "openrouter", "minimax", "ollama", "codex", "dockermr"}
}

func registerDefaults(r *Registry) {
	r.Register(Definition{
		Type: "claude",
		Detect: func(ctx context.Context, env DetectEnv) ([]llm.DetectedProvider, error) {
			if !claude.LocalTokenProviderAvailable() {
				return nil, nil
			}
			return []llm.DetectedProvider{{Name: "local", Type: "claude", Order: 10}}, nil
		},
		Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
			shared := append([]llm.Option{}, cfg.LLMOptions...)
			if cfg.HTTPClient != nil {
				shared = append(shared, llm.WithHTTPClient(cfg.HTTPClient))
			}
			if store, ok := cfg.Params["store"].(claude.TokenStore); ok {
				accountKey, _ := cfg.Params["accountKey"].(string)
				return claude.New(claude.WithManagedTokenProvider(accountKey, store, nil), claude.WithLLMOptions(shared...)), nil
			}
			return claude.New(claude.WithLocalTokenProvider(), claude.WithLLMOptions(shared...)), nil
		},
	})
	r.Register(Definition{Type: "anthropic", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "anthropic", Type: "anthropic", Order: 20}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		opts := []llm.Option{llm.APIKeyFromEnv("ANTHROPIC_API_KEY")}
		opts = append(opts, cfg.LLMOptions...)
		if cfg.HTTPClient != nil {
			opts = append(opts, llm.WithHTTPClient(cfg.HTTPClient))
		}
		return anthropic.New(opts...), nil
	}})
	r.Register(Definition{Type: "bedrock", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if os.Getenv("AWS_ACCESS_KEY_ID") == "" && os.Getenv("AWS_PROFILE") == "" && os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") == "" {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "bedrock", Type: "bedrock", Order: 30}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		var opts []bedrock.Option
		if cfg.HTTPClient != nil {
			opts = append(opts, bedrock.WithLLMOptions(llm.WithHTTPClient(cfg.HTTPClient)))
		}
		if len(cfg.LLMOptions) > 0 {
			opts = append(opts, bedrock.WithLLMOptions(cfg.LLMOptions...))
		}
		return bedrock.New(opts...), nil
	}})
	r.Register(Definition{Type: "openai", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if os.Getenv("OPENAI_API_KEY") == "" && os.Getenv("OPENAI_KEY") == "" {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "openai", Type: "openai", Order: 40}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		opts := append([]llm.Option{}, cfg.LLMOptions...)
		if cfg.HTTPClient != nil {
			opts = append(opts, llm.WithHTTPClient(cfg.HTTPClient))
		}
		return openai.New(opts...), nil
	}})
	r.Register(Definition{Type: "openrouter", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if os.Getenv("OPENROUTER_API_KEY") == "" {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "openrouter", Type: "openrouter", Order: 50}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		opts := []llm.Option{llm.APIKeyFromEnv("OPENROUTER_API_KEY")}
		opts = append(opts, cfg.LLMOptions...)
		if cfg.HTTPClient != nil {
			opts = append(opts, llm.WithHTTPClient(cfg.HTTPClient))
		}
		return openrouter.New(opts...), nil
	}})
	r.Register(Definition{Type: "minimax", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if os.Getenv("MINIMAX_API_KEY") == "" {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "minimax", Type: "minimax", Order: 60}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		var opts []minimax.Option
		if cfg.HTTPClient != nil {
			opts = append(opts, minimax.WithLLMOpts(llm.WithHTTPClient(cfg.HTTPClient)))
		}
		if len(cfg.LLMOptions) > 0 {
			opts = append(opts, minimax.WithLLMOpts(cfg.LLMOptions...))
		}
		return minimax.New(opts...), nil
	}})
	r.Register(Definition{Type: "ollama", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if !ollama.Available() {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "ollama", Type: "ollama", Params: map[string]any{"baseURL": ollama.BaseURL()}, Order: 70}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		opts := append([]llm.Option{}, cfg.LLMOptions...)
		if baseURL, _ := cfg.Params["baseURL"].(string); baseURL != "" {
			opts = append(opts, llm.WithBaseURL(baseURL))
		}
		if cfg.HTTPClient != nil {
			opts = append(opts, llm.WithHTTPClient(cfg.HTTPClient))
		}
		return ollama.New(opts...), nil
	}})
	r.Register(Definition{Type: "codex", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		if !codex.LocalAvailable() {
			return nil, nil
		}
		return []llm.DetectedProvider{{Name: "codex", Type: "codex", Order: 80}}, nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		auth, err := codex.LoadAuth()
		if err != nil {
			return nil, err
		}
		opts := append([]llm.Option{}, cfg.LLMOptions...)
		if cfg.HTTPClient != nil {
			opts = append(opts, llm.WithHTTPClient(cfg.HTTPClient))
		}
		return codex.New(auth, opts...), nil
	}})
	r.Register(Definition{Type: "dockermr", Detect: func(context.Context, DetectEnv) ([]llm.DetectedProvider, error) {
		var rt http.RoundTripper
		return detectDockerMR(rt), nil
	}, Build: func(ctx context.Context, cfg BuildConfig) (llm.Provider, error) {
		opts := append([]llm.Option{}, cfg.LLMOptions...)
		if cfg.HTTPClient != nil {
			opts = append(opts, llm.WithHTTPClient(cfg.HTTPClient))
		}
		return dockermr.New(opts...), nil
	}})
}

func detectDockerMR(sharedTransport http.RoundTripper) []llm.DetectedProvider {
	if dockermr.Available(sharedTransport) {
		return []llm.DetectedProvider{{Name: "dockermr", Type: "dockermr", Order: 90}}
	}
	return nil
}

func RegisterClaudeAccounts(ctx context.Context, store claude.TokenStore) ([]llm.DetectedProvider, error) {
	keys, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Strings(keys)
	out := make([]llm.DetectedProvider, 0, len(keys))
	for i, key := range keys {
		out = append(out, llm.DetectedProvider{Name: key, Type: "claude", Params: map[string]any{"store": store, "accountKey": key}, Order: 5 + i})
	}
	return out, nil
}
