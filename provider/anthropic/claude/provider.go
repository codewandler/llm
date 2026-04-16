package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/api/messages"
	"github.com/codewandler/llm/api/unified"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/provider/providercore"
	"github.com/codewandler/llm/tokencount"
	"github.com/codewandler/llm/usage"
)

const (
	providerName    = "claude"
	defaultBaseURL  = "https://api.anthropic.com"
	envBaseURL      = "ANTHROPIC_BASE_URL" // override for proxy/testing
	claudeUserAgent = "claude-cli/2.1.85 (external, sdk-cli)"
	claudeBeta      = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24"

	stainlessPackageVer = "0.74.0"
	stainlessNodeVer    = "v24.3.0"

	billingHeader = "x-anthropic-billing-header: cc_version=2.1.85.613; cc_entrypoint=sdk-cli; cch=1757e;"
	systemCore    = "You are a Claude agent, built on Anthropic's Claude Agent SDK."
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
	log           *slog.Logger
	tokenProvider TokenProvider
	userID        string
	sessionID     string
	initErr       error // set when a With* option fails to initialise its token provider

	*claudeModels
}

// New creates a new Claude OAuth provider.
// By default, if local Claude Code credentials exist (~/.claude/.credentials.json),
// they will be used automatically. Use WithTokenProvider() to override.
func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:      getEnvBaseURL(),
		client:       llm.DefaultHttpClient(),
		sessionID:    randomUUID(),
		claudeModels: newClaudeModels(),
		log:          slog.New(slog.DiscardHandler),
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

func (*Provider) CostCalculator() usage.CostCalculator {
	return usage.Default()
}

// CreateStream implements llm.Provider.
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	if p.initErr != nil {
		return nil, llm.NewErrProviderMsg(llm.ProviderNameClaude, p.initErr.Error())
	}
	return p.buildCore().Stream(ctx, src)
}

func (p *Provider) buildCore() *providercore.Client {
	cfg := providercore.Config{
		ProviderName: providerName,
		DefaultModel: ModelDefault,
		BaseURL:      p.baseURL,
		BasePath:     "/v1/messages",
		APIHint:      llm.ApiTypeAnthropicMessages,
		DefaultHeaders: http.Header{
			"Accept": {"application/json"},
		},
		CostCalculator: usage.Default(),
		TokenCounter:   p,
		RateLimitParser: func(resp *http.Response) *llm.RateLimits {
			if resp == nil {
				return nil
			}
			headers := make(map[string]string, len(resp.Header))
			for k, values := range resp.Header {
				if len(values) > 0 {
					headers[strings.ToLower(k)] = values[0]
				}
			}
			return llm.ParseRateLimits(headers)
		},
		HeaderFunc: func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			if p.tokenProvider == nil {
				return nil, llm.NewErrMissingAPIKey(llm.ProviderNameClaude)
			}
			token, err := p.tokenProvider.Token(ctx)
			if err != nil {
				return nil, llm.NewErrRequestFailed(llm.ProviderNameClaude, err)
			}
			return http.Header{"Authorization": {"Bearer " + token.AccessToken}}, nil
		},
		MutateRequest: func(r *http.Request) {
			p.setClaudeStaticHeaders(r)
			q := r.URL.Query()
			q.Set("beta", "true")
			r.URL.RawQuery = q.Encode()
		},
		PreprocessRequest: func(req llm.Request) (llm.Request, string, error) {
			normalizeRequest(&req)
			original := req.Model
			resolvedModel, err := p.Resolve(req.Model)
			if err != nil {
				return req, original, err
			}
			req.Model = resolvedModel.ID
			return req, original, nil
		},
		TransformWireRequest: func(api llm.ApiType, wire any) (any, error) {
			msgReq, ok := wire.(*messages.Request)
			if !ok {
				return nil, fmt.Errorf("unexpected messages payload %T", wire)
			}
			if err := p.augmentMessagesRequest(msgReq); err != nil {
				return nil, err
			}
			return msgReq, nil
		},
		APITokenCounter: func(ctx context.Context, _ llm.Request, wire any) (*tokencount.TokenCount, error) {
			msgReq, ok := wire.(*messages.Request)
			if !ok {
				return nil, fmt.Errorf("unexpected messages payload %T", wire)
			}
			count, err := p.countTokensAPI(ctx, msgReq)
			if err != nil {
				return nil, err
			}
			return &tokencount.TokenCount{InputTokens: count}, nil
		},
		ResolveHTTPErrorAction: func(_ llm.Request, statusCode int, _ error) providercore.HTTPErrorAction {
			if llm.IsRetriableHTTPStatus(statusCode) {
				return providercore.HTTPErrorActionReturn
			}
			return providercore.HTTPErrorActionStream
		},
	}

	return providercore.New(
		cfg,
		llm.WithBaseURL(p.baseURL),
		llm.WithHTTPClient(p.client),
		llm.WithLogger(p.log),
	)
}

// countTokensAPI calls /v1/messages/count_tokens with proper Claude OAuth headers.
func (p *Provider) countTokensAPI(ctx context.Context, apiReq *messages.Request) (int, error) {
	if p.tokenProvider == nil {
		return 0, fmt.Errorf("claude: count_tokens: missing token provider")
	}
	token, err := p.tokenProvider.Token(ctx)
	if err != nil {
		return 0, fmt.Errorf("claude: count_tokens: %w", err)
	}

	countReqBody, err := json.Marshal(struct {
		Model        string                    `json:"model"`
		Messages     []messages.Message        `json:"messages"`
		System       messages.SystemBlocks     `json:"system,omitempty"`
		Tools        []messages.ToolDefinition `json:"tools,omitempty"`
		ToolChoice   any                       `json:"tool_choice,omitempty"`
		Thinking     *messages.ThinkingConfig  `json:"thinking,omitempty"`
		CacheControl *messages.CacheControl    `json:"cache_control,omitempty"`
	}{
		Model:        apiReq.Model,
		Messages:     apiReq.Messages,
		System:       apiReq.System,
		Tools:        apiReq.Tools,
		ToolChoice:   apiReq.ToolChoice,
		Thinking:     apiReq.Thinking,
		CacheControl: apiReq.CacheControl,
	})
	if err != nil {
		return 0, fmt.Errorf("claude: count_tokens: %w", err)
	}

	endpoint := p.baseURL + "/v1/messages/count_tokens?beta=true"
	req, err := http.NewRequestWithContext(ctx, "POST",
		endpoint,
		bytes.NewReader(countReqBody))
	if err != nil {
		return 0, fmt.Errorf("claude: count_tokens: %w", err)
	}

	p.setClaudeHeaders(req, token.AccessToken)

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("claude: count_tokens: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("claude: count_tokens: HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("claude: count_tokens: decode: %w", err)
	}
	return result.InputTokens, nil
}

// setClaudeHeaders applies the full set of Claude OAuth headers to a request.
func (p *Provider) setClaudeHeaders(req *http.Request, accessToken string) {
	p.setClaudeStaticHeaders(req)
	req.Header.Set("Authorization", "Bearer "+accessToken)
}

func (p *Provider) setClaudeStaticHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Anthropic-Version", anthropic.AnthropicVersion)
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
	req.Header.Set("Connection", "keep-alive")
}

func (p *Provider) buildRequest(llmRequest llm.Request) (*messages.Request, error) {
	uReq, err := unified.RequestFromLLM(llmRequest)
	if err != nil {
		return nil, err
	}
	msgReq, err := unified.BuildMessagesRequest(uReq)
	if err != nil {
		return nil, err
	}
	if err := p.augmentMessagesRequest(msgReq); err != nil {
		return nil, err
	}
	return msgReq, nil
}

func (p *Provider) augmentMessagesRequest(msgReq *messages.Request) error {
	if msgReq == nil {
		return fmt.Errorf("nil messages request")
	}
	msgReq.System = append(messages.SystemBlocks{
		&messages.TextBlock{Type: messages.BlockTypeText, Text: billingHeader},
		&messages.TextBlock{Type: messages.BlockTypeText, Text: systemCore, CacheControl: &messages.CacheControl{Type: "ephemeral", TTL: "1h"}},
	}, msgReq.System...)
	if p.userID != "" {
		msgReq.Metadata = &messages.Metadata{UserID: p.userID}
	}
	return nil
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

	// Return JSON object format matching Claude Code
	id := map[string]string{
		"device_id":    cfg.UserID,
		"account_uuid": cfg.OAuthAccount.AccountUUID,
		"session_id":   p.sessionID,
	}

	data, err = json.Marshal(id)
	if err != nil {
		return ""
	}
	return string(data)
}

func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("anthropic: crypto/rand unavailable: " + err.Error())
	}
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

// getEnvBaseURL returns the base URL for API requests.
// Uses ANTHROPIC_BASE_URL environment variable if set, otherwise defaultBaseURL.
func getEnvBaseURL() string {
	if url := os.Getenv(envBaseURL); url != "" {
		return url
	}
	return defaultBaseURL
}
