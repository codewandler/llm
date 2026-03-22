package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/modeldb"
	"github.com/codewandler/llm/provider/anthropic"
)

const (
	providerName    = "claude"
	defaultBaseURL  = "https://api.anthropic.com"
	claudeUserAgent = "claude-cli/2.1.72 (external, sdk-cli)"
	claudeBeta      = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24"

	stainlessPackageVer = "0.74.0"
	stainlessNodeVer    = "v24.3.0"

	defaultModelSonnet = "claude-sonnet-4-6"
	defaultModelOpus   = "claude-opus-4-6"
	defaultModelHaiku  = "claude-haiku-4-5-20251001"

	billingHeader  = "x-anthropic-billing-header: cc_version=2.1.72.364; cc_entrypoint=sdk-cli;"
	systemCore     = "You are a Claude agent, built on Anthropic's Claude Agent SDK."
	systemIdentity = "You are Claude Code, Anthropic's official CLI for Claude."
)

// supportedModels is the allowlist of model IDs that work with Claude OAuth API.
var supportedModels = map[string]bool{
	// Claude 4.6 (current default)
	"claude-sonnet-4-6": true,
	"claude-opus-4-6":   true,

	// Claude 4.5
	"claude-sonnet-4-5":          true,
	"claude-sonnet-4-5-20250929": true,
	"claude-opus-4-5":            true,
	"claude-opus-4-5-20251101":   true,
	"claude-haiku-4-5":           true,
	"claude-haiku-4-5-20251001":  true,

	// Claude 4.1
	"claude-opus-4-1":          true,
	"claude-opus-4-1-20250805": true,

	// Claude 4.0
	"claude-sonnet-4":          true,
	"claude-sonnet-4-20250514": true,
	"claude-opus-4":            true,
	"claude-opus-4-20250514":   true,
}

// Provider implements Anthropic requests with Claude OAuth tokens.
type Provider struct {
	baseURL       string
	client        *http.Client
	tokenProvider TokenProvider
	userID        string
	sessionID     string
}

// Option configures the Claude provider.
type Option func(*Provider)

// WithLLMOptions applies one or more llm.Option values to the Claude provider.
// This allows using shared llm options (e.g. llm.WithHTTPClient) with this provider.
//
// Example:
//
//	claude.New(claude.WithLLMOptions(llm.WithHTTPClient(myClient)))
func WithLLMOptions(opts ...llm.Option) Option {
	return func(p *Provider) {
		cfg := llm.Apply(opts...)
		if cfg.HTTPClient != nil {
			p.client = cfg.HTTPClient
		}
	}
}

// WithTokenProvider sets a custom token provider.
func WithTokenProvider(tp TokenProvider) Option {
	return func(p *Provider) {
		p.tokenProvider = tp
	}
}

// WithManagedTokenProvider creates and sets a managed token provider.
func WithManagedTokenProvider(key string, store TokenStore, onRefreshed OnTokenRefreshed) Option {
	return func(p *Provider) {
		p.tokenProvider = NewManagedTokenProvider(key, store, onRefreshed)
	}
}

// WithLocalTokenProvider sets the local Claude Code token provider.
// This reads tokens from ~/.claude/.credentials.json (or CLAUDE_CONFIG_DIR)
// and automatically refreshes expired tokens, writing them back to the file.
func WithLocalTokenProvider() Option {
	return func(p *Provider) {
		tp, err := NewLocalTokenProvider()
		if err != nil {
			// Don't fail - let CreateStream report the error
			return
		}
		p.tokenProvider = tp
	}
}

// WithClaudeDir sets a custom Claude config directory for local credentials.
// The directory should contain .credentials.json file.
func WithClaudeDir(dir string) Option {
	return func(p *Provider) {
		tp, err := NewLocalTokenProviderWithDir(dir)
		if err != nil {
			// Don't fail - let CreateStream report the error
			return
		}
		p.tokenProvider = tp
	}
}

// WithBaseURL sets a custom base URL for the API.
func WithBaseURL(url string) Option {
	return func(p *Provider) {
		p.baseURL = url
	}
}

// New creates a new Claude OAuth provider.
// By default, if local Claude Code credentials exist (~/.claude/.credentials.json),
// they will be used automatically. Use WithTokenProvider() to override.
func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:   defaultBaseURL,
		client:    llm.DefaultHttpClient(),
		sessionID: randomUUID(),
	}

	// Default to local token provider if available
	if LocalTokenProviderAvailable() {
		if tp, err := NewLocalTokenProvider(); err == nil {
			p.tokenProvider = tp
		}
	}

	// Apply user options (may override token provider)
	for _, opt := range opts {
		opt(p)
	}

	p.userID = p.buildUserID()
	return p
}

// Name returns the provider name.
func (p *Provider) Name() string { return providerName }

// Models returns available models.
func (p *Provider) Models() []llm.Model {
	models := modelsFromDB()
	if len(models) > 0 {
		return models
	}

	return []llm.Model{
		{ID: defaultModelSonnet, Name: "Claude Sonnet 4.6", Provider: providerName},
		{ID: defaultModelOpus, Name: "Claude Opus 4.6", Provider: providerName},
		{ID: defaultModelHaiku, Name: "Claude Haiku 4.5", Provider: providerName},
	}
}

// CreateStream implements llm.Provider.
func (p *Provider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	startTime := time.Now()
	requestedModel := opts.Model

	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	if p.tokenProvider == nil {
		return nil, fmt.Errorf("no token provider configured; use claude.WithTokenProvider() or claude.WithManagedTokenProvider()")
	}

	token, err := p.tokenProvider.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	opts.Model = normalizeModel(opts.Model)
	body, err := p.buildRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := p.newAPIRequest(ctx, token.AccessToken, body)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("claude request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go anthropic.ParseStream(ctx, resp.Body, events, anthropic.StreamMeta{
		RequestedModel: requestedModel,
		ResolvedModel:  opts.Model,
		StartTime:      startTime,
	})
	return events, nil
}

func (p *Provider) newAPIRequest(ctx context.Context, accessToken string, body []byte) (*http.Request, error) {
	endpoint := p.baseURL + "/v1/messages?beta=true"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Beta", claudeBeta)
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	req.Header.Set("User-Agent", claudeUserAgent)
	req.Header.Set("X-App", "cli")
	req.Header.Set("X-Stainless-Lang", "js")
	req.Header.Set("X-Stainless-Os", stainlessOS())
	req.Header.Set("X-Stainless-Arch", stainlessArch())
	req.Header.Set("X-Stainless-Package-Version", stainlessPackageVer)
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-Stainless-Runtime", "node")
	req.Header.Set("X-Stainless-Runtime-Version", stainlessNodeVer)
	req.Header.Set("X-Stainless-Timeout", "600")
	return req, nil
}

func (p *Provider) buildRequest(opts llm.StreamOptions) ([]byte, error) {
	// Prepend Claude-specific system blocks
	userBlocks := anthropic.CollectSystemBlocks(opts.Messages)
	systemBlocks := anthropic.PrependSystemBlocks(
		[]anthropic.SystemBlock{
			{Type: "text", Text: billingHeader},
			{Type: "text", Text: systemCore},
			{Type: "text", Text: systemIdentity},
		},
		userBlocks,
	)

	return anthropic.BuildRequest(anthropic.RequestOptions{
		Model:         opts.Model,
		SystemBlocks:  systemBlocks,
		UserID:        p.userID,
		StreamOptions: opts,
	})
}

func (p *Provider) buildUserID() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return ""
	}

	var cfg struct {
		UserID       string `json:"userID"`
		OAuthAccount struct {
			AccountUUID string `json:"accountUuid"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.UserID == "" {
		return ""
	}

	id := "user_" + cfg.UserID
	if cfg.OAuthAccount.AccountUUID != "" {
		id += "_account_" + cfg.OAuthAccount.AccountUUID
	}
	id += "_session_" + p.sessionID
	return id
}

func modelsFromDB() []llm.Model {
	provider, ok := modeldb.GetProvider("anthropic")
	if !ok || len(provider.Models) == 0 {
		return nil
	}

	ids := make([]string, 0, len(supportedModels))
	for id := range provider.Models {
		if supportedModels[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	sort.Strings(ids)
	out := make([]llm.Model, 0, len(ids))
	for _, id := range ids {
		m := provider.Models[id]
		name := m.Name
		if name == "" {
			name = id
		}
		out = append(out, llm.Model{ID: id, Name: name, Provider: providerName})
	}

	return out
}

func normalizeModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "sonnet":
		return defaultModelSonnet
	case "opus":
		return defaultModelOpus
	case "haiku":
		return defaultModelHaiku
	default:
		return model
	}
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func stainlessOS() string {
	switch runtime.GOOS {
	case "darwin":
		return "MacOS"
	case "windows":
		return "Windows"
	default:
		return "Linux"
	}
}

func stainlessArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x64"
}
