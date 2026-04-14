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

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic"
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
	req, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameClaude, err)
	}

	if p.initErr != nil {
		return nil, llm.NewErrProviderMsg(llm.ProviderNameClaude, p.initErr.Error())
	}

	normalizeRequest(&req)

	// Use the provider default when no model is specified.
	// Validation below requires a non-empty model, so this must come first.
	if req.Model == "" {
		req.Model = ModelDefault
	}

	// Resolve model to include inference profile prefix.

	resolvedModel, err := p.Resolve(req.Model)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameClaude, err)
	}
	req.Model = resolvedModel.ID

	if err := req.Validate(); err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameClaude, err)
	}

	if p.tokenProvider == nil {
		return nil, llm.NewErrMissingAPIKey(llm.ProviderNameClaude)
	}

	token, err := p.tokenProvider.Token(ctx)
	if err != nil {
		return nil, llm.NewErrRequestFailed(llm.ProviderNameClaude, err)
	}

	// Build request
	requestBody, err := p.buildRequest(req)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameClaude, err)
	}

	requestBodyBytes, err := json.MarshalIndent(requestBody, "", "  ")
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameClaude, err)
	}
	p.log.Debug("request body", "body", string(requestBodyBytes))

	// Build http.Request first so URL, method, and headers are available for
	// the RequestEvent. The request is fully constructed here but not yet sent.
	httpReq, err := p.newAPIRequest(ctx, token.AccessToken, requestBodyBytes)
	if err != nil {
		return nil, llm.NewErrBuildRequest(llm.ProviderNameClaude, err)
	}

	parseOpts := anthropic.ParseOpts{
		Model:         req.Model,
		ProviderName:  providerName,
		LLMRequest:    req,
		RequestParams: llm.ProviderRequestFromHTTP(httpReq, requestBodyBytes),
	}

	// Create publisher and emit RequestEvent BEFORE the HTTP call.
	pub, ch := llm.NewEventPublisher()
	anthropic.PublishRequestParams(pub, parseOpts)

	// Emit token estimates: heuristic (local BPE) + API (exact count).
	// Both are emitted so consumers can compare drift between the two methods.

	// 1. Heuristic estimate (local, no network call)
	if est, err := p.CountTokens(ctx, tokencount.TokenCountRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Tools:    req.Tools,
	}); err == nil {
		for _, rec := range tokencount.EstimateRecords(est, llm.ProviderNameClaude, req.Model, "heuristic", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	// 2. API count (exact, free endpoint — adds one HTTP round-trip).
	// Build a proper Claude OAuth request with all required headers.
	if apiCount, err := p.countTokensAPI(ctx, token.AccessToken, requestBody); err == nil {
		apiEst := &tokencount.TokenCount{InputTokens: apiCount}
		for _, rec := range tokencount.EstimateRecords(apiEst, llm.ProviderNameClaude, req.Model, "api", usage.Default()) {
			pub.TokenEstimate(rec)
		}
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		pub.Close()
		return nil, llm.NewErrRequestFailed(llm.ProviderNameClaude, err)
	}

	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // intentional: defer Close is only for cleanup, failure after response reading is non-fatal
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		apiErr := llm.NewErrAPIErrorWithRequest(llm.ProviderNameClaude, string(requestBodyBytes), resp.StatusCode, string(errBody))
		if llm.IsRetriableHTTPStatus(resp.StatusCode) {
			// Retriable: return error so the router can try the next provider.
			// Events already emitted (RequestEvent) are lost, but failover is more important.
			pub.Close()
			return nil, apiErr
		}
		// Non-retriable: surface the error through the stream so all
		// preamble events (RequestEvent etc.) remain visible to the caller.
		pub.Error(apiErr)
		pub.Close()
		return ch, nil
	}

	anthropic.ParseStreamWith(ctx, resp.Body, pub, parseOpts)
	return ch, nil
}

func (p *Provider) newAPIRequest(ctx context.Context, accessToken string, body []byte) (*http.Request, error) {
	endpoint := p.baseURL + "/v1/messages?beta=true"
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	p.setClaudeHeaders(req, accessToken)
	return req, nil
}

// countTokensAPI calls /v1/messages/count_tokens with proper Claude OAuth headers.
func (p *Provider) countTokensAPI(ctx context.Context, accessToken string, apiReq anthropic.Request) (int, error) {
	countReqBody, err := json.Marshal(struct {
		Model        string                     `json:"model"`
		Messages     []anthropic.Message        `json:"messages"`
		System       anthropic.SystemBlocks     `json:"system,omitempty"`
		Tools        []anthropic.ToolDefinition `json:"tools,omitempty"`
		ToolChoice   any                        `json:"tool_choice,omitempty"`
		Thinking     *anthropic.ThinkingConfig  `json:"thinking,omitempty"`
		CacheControl *anthropic.CacheControl    `json:"cache_control,omitempty"`
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

	req, err := http.NewRequestWithContext(ctx, "POST",
		p.baseURL+"/v1/messages/count_tokens?beta=true",
		bytes.NewReader(countReqBody))
	if err != nil {
		return 0, fmt.Errorf("claude: count_tokens: %w", err)
	}

	p.setClaudeHeaders(req, accessToken)

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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Authorization", "Bearer "+accessToken)
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

func (p *Provider) buildRequest(llmRequest llm.Request) (anthropic.Request, error) {
	return anthropic.BuildRequest(anthropic.RequestOptions{
		SystemBlocks: anthropic.SystemBlocks{
			anthropic.Text(billingHeader),
			anthropic.Text(systemCore).WithCacheControl(&anthropic.CacheControl{Type: "ephemeral", TTL: "1h"}),
		},
		UserID:     p.userID,
		LLMRequest: llmRequest,
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
