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

	agentmessages "github.com/codewandler/agentapis/api/messages"
	"github.com/codewandler/llm"
	providercore2 "github.com/codewandler/llm/internal/providercore"
	"github.com/codewandler/llm/provider/anthropic"
	"github.com/codewandler/llm/tokencount"
)

const (
	providerName    = "claude"
	defaultBaseURL  = "https://api.anthropic.com"
	envBaseURL      = "ANTHROPIC_BASE_URL"
	claudeUserAgent = "claude-cli/2.1.85 (external, sdk-cli)"
	claudeBeta      = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,effort-2025-11-24"

	stainlessPackageVer = "0.74.0"
	stainlessNodeVer    = "v24.3.0"

	billingHeader = "x-anthropic-billing-header: cc_version=2.1.85.613; cc_entrypoint=sdk-cli; cch=1757e;"
	systemCore    = "You are a Claude agent, built on Anthropic's Claude Agent SDK."
)

var supportedModels = map[string]bool{
	"claude-sonnet-4-6": true,
	"claude-opus-4-6":   true,

	"claude-sonnet-4-5":          true,
	"claude-sonnet-4-5-20250929": true,
	"claude-opus-4-5":            true,
	"claude-opus-4-5-20251101":   true,
	"claude-haiku-4-5":           true,
	"claude-haiku-4-5-20251001":  true,

	"claude-opus-4-1":          true,
	"claude-opus-4-1-20250805": true,

	"claude-sonnet-4":          true,
	"claude-sonnet-4-20250514": true,
	"claude-opus-4":            true,
	"claude-opus-4-20250514":   true,
}

type Provider struct {
	baseURL       string
	client        *http.Client
	log           *slog.Logger
	tokenProvider TokenProvider
	userID        string
	sessionID     string
	initErr       error

	inner                  *providercore2.Provider
	claudeModels           *claudeModels
	autoSystemCacheControl *providercore2.MessagesCacheControl
}

func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:      getEnvBaseURL(),
		client:       llm.DefaultHttpClient(),
		sessionID:    randomUUID(),
		claudeModels: newClaudeModels(),
		log:          slog.New(slog.DiscardHandler),
	}

	if LocalTokenProviderAvailable() {
		if tp, err := NewLocalTokenProvider(); err == nil {
			p.tokenProvider = tp
		}
	}

	for _, opt := range opts {
		opt(p)
	}

	p.userID = p.buildUserID()

	p.inner = providercore2.NewProvider(providercore2.NewOptions(
		providercore2.WithProviderName(providerName),
		providercore2.WithBaseURLFunc(func() string { return p.baseURL }),
		providercore2.WithAPIHint(llm.ApiTypeAnthropicMessages),
		providercore2.WithCachedModelsFunc(func(ctx context.Context) (llm.Models, error) {
			return p.claudeModels.Models(), nil
		}),
		providercore2.WithDefaultHeaders(http.Header{
			"Accept": {"application/json"},
		}),
		providercore2.WithHeaderFunc(func(ctx context.Context, _ *llm.Request) (http.Header, error) {
			if p.tokenProvider == nil {
				return nil, llm.NewErrMissingAPIKey(llm.ProviderNameClaude)
			}
			token, err := p.tokenProvider.Token(ctx)
			if err != nil {
				return nil, llm.NewErrRequestFailed(llm.ProviderNameClaude, err)
			}
			return http.Header{"Authorization": {"Bearer " + token.AccessToken}}, nil
		}),
		providercore2.WithMutateRequest(func(r *http.Request) {
			p.setClaudeStaticHeaders(r)
			q := r.URL.Query()
			q.Set("beta", "true")
			r.URL.RawQuery = q.Encode()
		}),
		providercore2.WithPreprocessRequest(func(req llm.Request) (llm.Request, string, error) {
			normalizeRequest(&req)
			original := req.Model
			resolvedModel, err := p.claudeModels.Resolve(req.Model)
			if err != nil {
				return req, original, err
			}
			req.Model = resolvedModel.ID
			return req, original, nil
		}),
		providercore2.WithMessagesRequestTransform(func(msgReq *providercore2.MessagesRequest) error {
			anthropic.CoerceAnthropicThinkingTemperature(msgReq)
			if err := p.augmentMessagesRequest(msgReq); err != nil {
				return err
			}
			return nil
		}),
		providercore2.WithMessagesAPITokenCounter(func(ctx context.Context, _ llm.Request, msgReq *providercore2.MessagesRequest) (*tokencount.TokenCount, error) {
			count, err := p.countTokensAPI(ctx, msgReq)
			if err != nil {
				return nil, err
			}
			return &tokencount.TokenCount{InputTokens: count}, nil
		}),
		providercore2.WithRateLimitParser(func(resp *http.Response) *llm.RateLimits {
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
		}),
		providercore2.WithHTTPErrorActionResolver(func(_ llm.Request, statusCode int, _ error) providercore2.HTTPErrorAction {
			if llm.IsRetriableHTTPStatus(statusCode) {
				return providercore2.HTTPErrorActionReturn
			}
			return providercore2.HTTPErrorActionStream
		}),
	), llm.WithHTTPClient(p.client), llm.WithLogger(p.log))

	return p
}

func (p *Provider) Name() string       { return p.inner.Name() }
func (p *Provider) Models() llm.Models { return p.inner.Models() }
func (p *Provider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	if p.initErr != nil {
		return nil, llm.NewErrProviderMsg(llm.ProviderNameClaude, p.initErr.Error())
	}
	return p.inner.CreateStream(ctx, src)
}

func (p *Provider) countTokensAPI(ctx context.Context, apiReq *providercore2.MessagesRequest) (int, error) {
	if p.tokenProvider == nil {
		return 0, fmt.Errorf("claude: count_tokens: missing token provider")
	}
	token, err := p.tokenProvider.Token(ctx)
	if err != nil {
		return 0, fmt.Errorf("claude: count_tokens: %w", err)
	}

	countReqBody, err := json.Marshal(struct {
		Model        string                                 `json:"model"`
		Messages     []providercore2.MessagesMessage        `json:"messages"`
		System       providercore2.MessagesSystemBlocks     `json:"system,omitempty"`
		Tools        []providercore2.MessagesToolDefinition `json:"tools,omitempty"`
		ToolChoice   any                                    `json:"tool_choice,omitempty"`
		Thinking     *providercore2.MessagesThinkingConfig  `json:"thinking,omitempty"`
		CacheControl *providercore2.MessagesCacheControl    `json:"cache_control,omitempty"`
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

func (p *Provider) augmentMessagesRequest(msgReq *providercore2.MessagesRequest) error {
	if msgReq == nil {
		return fmt.Errorf("nil messages request")
	}
	msgReq.System = append(providercore2.MessagesSystemBlocks{
		&agentmessages.TextBlock{Type: agentmessages.BlockTypeText, Text: billingHeader},
		&agentmessages.TextBlock{Type: agentmessages.BlockTypeText, Text: systemCore},
	}, msgReq.System...)
	if p.autoSystemCacheControl != nil && len(msgReq.System) > 1 && msgReq.System[1] != nil && msgReq.System[1].CacheControl == nil {
		msgReq.System[1].CacheControl = &agentmessages.CacheControl{Type: p.autoSystemCacheControl.Type, TTL: p.autoSystemCacheControl.TTL}
	}
	if p.userID != "" {
		msgReq.Metadata = &agentmessages.Metadata{UserID: p.userID}
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

func getEnvBaseURL() string {
	if url := os.Getenv(envBaseURL); url != "" {
		return url
	}
	return defaultBaseURL
}
