package anthropic

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
	"sync"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/modeldb"
)

const (
	claudeCodeProviderName = "anthropic:claude-code"
	claudeCodeUserAgent    = "claude-cli/2.1.72 (external, sdk-cli)"
	claudeCodeBeta         = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24"
	stainlessPackageVer    = "0.74.0"
	stainlessNodeVer       = "v24.3.0"

	claudeCodeModelSonnet = "claude-sonnet-4-6"
	claudeCodeModelOpus   = "claude-opus-4-6"
	claudeCodeModelHaiku  = "claude-haiku-4-5-20251001"

	ccBillingHeader  = "x-anthropic-billing-header: cc_version=2.1.72.364; cc_entrypoint=sdk-cli;"
	ccSystemCore     = "You are a Claude agent, built on Anthropic's Claude Agent SDK."
	ccSystemIdentity = "You are Claude Code, Anthropic's official CLI for Claude."
)

// OAuthConfig holds OAuth token information for Claude Code authentication.
type OAuthConfig struct {
	Access  string
	Refresh string
	Expires int64
}

// ClaudeProvider implements Anthropic requests with Claude Code credentials/profile.
type ClaudeProvider struct {
	opts       *llm.Options
	client     *http.Client
	oauthPath  string
	oauth      *OAuthConfig
	oauthReady bool
	userID     string
	sessionID  string
	mu         sync.Mutex
}

// NewClaudeCodeProvider creates a Claude Code profile provider.
func NewClaudeCodeProvider(opts ...llm.Option) *ClaudeProvider {
	allOpts := append(DefaultOptions(), opts...)
	cfg := llm.Apply(allOpts...)
	p := &ClaudeProvider{
		opts:      cfg,
		client:    &http.Client{},
		oauthPath: defaultClaudeCredentialsPath(),
		sessionID: randomUUID(),
	}
	p.userID = p.buildUserID()
	return p
}

func (p *ClaudeProvider) Name() string { return claudeCodeProviderName }

func (p *ClaudeProvider) Models() []llm.Model {
	models := claudeCodeModelsFromDB()
	if len(models) > 0 {
		return models
	}

	return []llm.Model{
		{ID: claudeCodeModelSonnet, Name: "Claude Sonnet 4.6", Provider: claudeCodeProviderName},
		{ID: claudeCodeModelOpus, Name: "Claude Opus 4.6", Provider: claudeCodeProviderName},
		{ID: claudeCodeModelHaiku, Name: "Claude Haiku 4.5", Provider: claudeCodeProviderName},
	}
}

// claudeCodeSupportedModels is the allowlist of model IDs that work with Claude Code API.
// These were extracted from the Claude CLI binary and verified via API testing.
// Older 3.x models return 404 errors via the Claude Code endpoint.
var claudeCodeSupportedModels = map[string]bool{
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

func claudeCodeModelsFromDB() []llm.Model {
	provider, ok := modeldb.GetProvider("anthropic")
	if !ok || len(provider.Models) == 0 {
		return nil
	}

	ids := make([]string, 0, len(claudeCodeSupportedModels))
	for id := range provider.Models {
		if claudeCodeSupportedModels[id] {
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
		out = append(out, llm.Model{ID: id, Name: name, Provider: claudeCodeProviderName})
	}

	return out
}

func normalizeClaudeCodeModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "sonnet":
		return claudeCodeModelSonnet
	case "opus":
		return claudeCodeModelOpus
	case "haiku":
		return claudeCodeModelHaiku
	default:
		return model
	}
}

func (p *ClaudeProvider) CreateStream(ctx context.Context, opts llm.StreamOptions) (<-chan llm.StreamEvent, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid options: %w", err)
	}

	token, err := p.getAccessToken()
	if err != nil {
		return nil, err
	}

	opts.Model = normalizeClaudeCodeModel(opts.Model)
	body, err := p.buildClaudeRequest(opts)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := p.newClaudeAPIRequest(ctx, token, body)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (HTTP %d): %s", resp.StatusCode, string(errBody))
	}

	events := make(chan llm.StreamEvent, 64)
	go parseStream(ctx, resp.Body, events)
	return events, nil
}

func (p *ClaudeProvider) getAccessToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.oauthReady {
		oauth, err := loadOAuthConfigFromPath(p.oauthPath)
		if err != nil {
			return "", err
		}
		p.oauth = oauth
		p.oauthReady = true
	}

	if p.oauth == nil {
		return "", fmt.Errorf("claude code credentials not configured")
	}
	if p.oauth.IsExpired() {
		return "", fmt.Errorf("claude code token is expired (expiresAt=%d); refresh is not implemented yet", p.oauth.Expires)
	}
	return p.oauth.Access, nil
}

func (p *ClaudeProvider) newClaudeAPIRequest(ctx context.Context, token string, body []byte) (*http.Request, error) {
	endpoint := p.opts.BaseURL + "/v1/messages?beta=true"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Anthropic-Version", anthropicVersion)
	req.Header.Set("Anthropic-Beta", claudeCodeBeta)
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	req.Header.Set("User-Agent", claudeCodeUserAgent)
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

func (p *ClaudeProvider) buildClaudeRequest(opts llm.StreamOptions) ([]byte, error) {
	r := request{Model: opts.Model, MaxTokens: 16384, Stream: true}
	r.System = claudeCodeSystemBlocks("")
	if p.userID != "" {
		r.Metadata = &metadata{UserID: p.userID}
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{Name: t.Name, Description: t.Description, InputSchema: t.Parameters})
	}

	if len(opts.Tools) > 0 {
		switch tc := opts.ToolChoice.(type) {
		case nil, llm.ToolChoiceAuto:
			r.ToolChoice = map[string]string{"type": "auto"}
		case llm.ToolChoiceRequired:
			r.ToolChoice = map[string]string{"type": "any"}
		case llm.ToolChoiceNone:
		case llm.ToolChoiceTool:
			r.ToolChoice = map[string]any{"type": "tool", "name": tc.Name}
		}
	}

	for i := 0; i < len(opts.Messages); i++ {
		switch m := opts.Messages[i].(type) {
		case *llm.SystemMsg:
			r.System = claudeCodeSystemBlocks(m.Content)
		case *llm.UserMsg:
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: m.Content})
		case *llm.AssistantMsg:
			if len(m.ToolCalls) == 0 {
				r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: m.Content})
				continue
			}
			var blocks []any
			if m.Content != "" {
				blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, toolUseBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: ensureInputMap(tc.Arguments)})
			}
			r.Messages = append(r.Messages, messagePayload{Role: "assistant", Content: blocks})
		case *llm.ToolCallResult:
			var results []contentBlock
			prevAssistant := findPrecedingAssistant(opts.Messages, i)
			toolIdx := 0
			for ; i < len(opts.Messages); i++ {
				tr, ok := opts.Messages[i].(*llm.ToolCallResult)
				if !ok {
					break
				}
				toolUseID := tr.ToolCallID
				if toolUseID == "" && prevAssistant != nil && toolIdx < len(prevAssistant.ToolCalls) {
					toolUseID = prevAssistant.ToolCalls[toolIdx].ID
				}
				results = append(results, contentBlock{Type: "tool_result", ToolUseID: toolUseID, Content: tr.Output, IsError: tr.IsError})
				toolIdx++
			}
			i--
			r.Messages = append(r.Messages, messagePayload{Role: "user", Content: results})
		}
	}

	return json.Marshal(r)
}

func claudeCodeSystemBlocks(userSystem string) []systemBlock {
	blocks := []systemBlock{
		{Type: "text", Text: ccBillingHeader},
		{Type: "text", Text: ccSystemCore},
		{Type: "text", Text: ccSystemIdentity},
	}
	if strings.TrimSpace(userSystem) != "" {
		blocks = append(blocks, systemBlock{Type: "text", Text: userSystem})
	}
	return blocks
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (p *ClaudeProvider) buildUserID() string {
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
