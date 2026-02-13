package anthropic

import (
	"bufio"
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
	"strings"
	"sync"
	"time"

	"github.com/codewandler/llm"
)

const (
	anthropicVersion       = "2023-06-01"
	anthropicBeta          = "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05"
	claudeCodeSystemPrefix = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeUserAgent    = "claude-cli/2.1.38 (external, cli)"
	stainlessPackageVer    = "0.73.0"
	stainlessNodeVer       = "v24.3.0"
	toolPrefix             = "mcp_"
	tokenRefreshURL        = "https://console.anthropic.com/v1/oauth/token"
)

// Provider implements the Anthropic (Claude) LLM backend.
type Provider struct {
	config       *llm.ProviderConfig
	baseURL      string
	client       *http.Client
	mu           sync.Mutex // protects token refresh
	userID       string     // compound metadata user_id
	sessionID    string     // random per-instance session UUID
	quotaChecked bool
}

// New creates a new Anthropic provider.
func New(cfg *llm.ProviderConfig, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	p := &Provider{
		config:    cfg,
		baseURL:   baseURL,
		client:    &http.Client{},
		sessionID: randomUUID(),
	}
	p.userID = p.buildUserID()
	return p
}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) Models() []llm.Model {
	return []llm.Model{
		{ID: "claude-sonnet-4-5-20250929", Name: "Claude Sonnet 4.5", Provider: "anthropic"},
		{ID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Provider: "anthropic"},
	}
}

func (p *Provider) isOAuth() bool {
	token := p.config.GetAccessToken()
	return strings.HasPrefix(token, "sk-ant-oat")
}

func (p *Provider) getAccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// If using OAuth, check expiry and refresh if needed.
	if p.config.OAuth != nil {
		now := time.Now().UnixMilli()
		// Refresh if expired or within 5 minutes of expiry.
		if now >= p.config.OAuth.Expires-5*60*1000 {
			if err := p.refreshToken(ctx); err != nil {
				return "", fmt.Errorf("token refresh: %w", err)
			}
		}
		return p.config.OAuth.Access, nil
	}

	return p.config.APIKey, nil
}

func (p *Provider) refreshToken(ctx context.Context) error {
	if p.config.OAuth == nil || p.config.OAuth.Refresh == "" {
		return fmt.Errorf("no refresh token available")
	}

	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": p.config.OAuth.Refresh,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", tokenRefreshURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"` // seconds
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Update stored tokens.
	p.config.OAuth.Access = result.AccessToken
	p.config.OAuth.Refresh = result.RefreshToken
	p.config.OAuth.Expires = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).UnixMilli()

	return nil
}

func (p *Provider) SendMessage(ctx context.Context, opts llm.SendOptions) (<-chan llm.StreamEvent, error) {
	oauth := p.isOAuth()

	token, err := p.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	// Quota check: Claude Code sends a lightweight request first.
	if oauth && !p.quotaChecked {
		p.quotaChecked = true
		if err := p.checkQuota(ctx, token); err != nil {
			return nil, fmt.Errorf("quota check: %w", err)
		}
	}

	body, err := p.buildRequest(opts, oauth)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	req, err := p.newAPIRequest(ctx, token, oauth, body)
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
	go parseStream(resp.Body, events, oauth)
	return events, nil
}

// checkQuota sends the initial lightweight request that Claude Code sends
// to verify quota/access before the real conversation.
func (p *Provider) checkQuota(ctx context.Context, token string) error {
	body, _ := json.Marshal(map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "quota"}},
		"metadata":   map[string]string{"user_id": p.userID},
	})

	req, err := p.newAPIRequest(ctx, token, true, body)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("quota check failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// newAPIRequest builds an HTTP request with all Claude Code headers.
func (p *Provider) newAPIRequest(ctx context.Context, token string, oauth bool, body []byte) (*http.Request, error) {
	endpoint := p.baseURL + "/v1/messages"
	if oauth {
		endpoint += "?beta=true"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Anthropic-Version", anthropicVersion)
	if oauth {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Anthropic-Beta", anthropicBeta)
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
	} else {
		req.Header.Set("x-api-key", token)
	}

	return req, nil
}

// --- Request building ---

type request struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Stream    bool             `json:"stream"`
	System    any              `json:"system,omitempty"`
	Messages  []messagePayload `json:"messages"`
	Tools     []toolPayload    `json:"tools,omitempty"`
	Thinking  *thinkingConfig  `json:"thinking,omitempty"`
	Metadata  *metadata        `json:"metadata,omitempty"`
}

type metadata struct {
	UserID string `json:"user_id"`
}

type thinkingConfig struct {
	Type        string `json:"type"`
	BudgetToken int    `json:"budget_tokens"`
}

type systemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type messagePayload struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type toolPayload struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

func maybePrefixTool(name string, oauth bool) string {
	if oauth {
		return toolPrefix + name
	}
	return name
}

func maybeStripToolPrefix(name string) string {
	return strings.TrimPrefix(name, toolPrefix)
}

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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
	if json.Unmarshal(data, &cfg) != nil || cfg.UserID == "" {
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
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	default:
		return "x64"
	}
}

func (p *Provider) buildRequest(opts llm.SendOptions, oauth bool) ([]byte, error) {
	r := request{
		Model:     opts.Model,
		MaxTokens: 16384,
		Stream:    true,
	}

	if oauth && p.userID != "" {
		r.Metadata = &metadata{UserID: p.userID}
	}

	for _, t := range opts.Tools {
		r.Tools = append(r.Tools, toolPayload{
			Name:        maybePrefixTool(t.Name, oauth),
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	for i := 0; i < len(opts.Messages); i++ {
		msg := opts.Messages[i]

		switch msg.Role {
		case llm.RoleSystem:
			if oauth {
				r.System = []systemBlock{
					{Type: "text", Text: claudeCodeSystemPrefix + "\n\n" + msg.Content},
				}
			} else {
				r.System = msg.Content
			}

		case llm.RoleUser:
			r.Messages = append(r.Messages, messagePayload{
				Role:    "user",
				Content: msg.Content,
			})

		case llm.RoleAssistant:
			if len(msg.ToolCalls) == 0 {
				r.Messages = append(r.Messages, messagePayload{
					Role:    "assistant",
					Content: msg.Content,
				})
			} else {
				var blocks []contentBlock
				if msg.Content != "" {
					blocks = append(blocks, contentBlock{Type: "text", Text: msg.Content})
				}
				for _, tc := range msg.ToolCalls {
					blocks = append(blocks, contentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  maybePrefixTool(tc.Name, oauth),
						Input: tc.Arguments,
					})
				}
				r.Messages = append(r.Messages, messagePayload{
					Role:    "assistant",
					Content: blocks,
				})
			}

		case llm.RoleTool:
			var results []contentBlock
			prevAssistant := findPrecedingAssistant(opts.Messages, i)
			toolIdx := 0
			for ; i < len(opts.Messages) && opts.Messages[i].Role == llm.RoleTool; i++ {
				toolUseID := ""
				if prevAssistant != nil && toolIdx < len(prevAssistant.ToolCalls) {
					toolUseID = prevAssistant.ToolCalls[toolIdx].ID
				}
				block := contentBlock{
					Type:      "tool_result",
					ToolUseID: toolUseID,
					Content:   opts.Messages[i].Content,
				}
				if prevAssistant != nil && toolIdx < len(prevAssistant.ToolCalls) {
					tc := prevAssistant.ToolCalls[toolIdx]
					if tc.Result != nil && tc.Result.IsError {
						block.IsError = true
					}
				}
				results = append(results, block)
				toolIdx++
			}
			i--
			r.Messages = append(r.Messages, messagePayload{
				Role:    "user",
				Content: results,
			})
		}
	}

	return json.Marshal(r)
}

func findPrecedingAssistant(messages []llm.Message, toolIdx int) *llm.Message {
	for j := toolIdx - 1; j >= 0; j-- {
		if messages[j].Role == llm.RoleAssistant {
			return &messages[j]
		}
	}
	return nil
}

// --- SSE stream parsing ---

type contentBlockStartEvent struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"content_block"`
}

type contentBlockDeltaEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
	} `json:"delta"`
}

func parseStream(body io.ReadCloser, events chan<- llm.StreamEvent, oauth bool) {
	defer close(events)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	type toolBlock struct {
		id      string
		name    string
		jsonBuf strings.Builder
	}
	activeTools := make(map[int]*toolBlock)
	var usage llm.Usage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &base); err != nil {
			continue
		}

		switch base.Type {
		case "message_start":
			var evt struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				usage.InputTokens = evt.Message.Usage.InputTokens
			}

		case "message_delta":
			var evt struct {
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				usage.OutputTokens = evt.Usage.OutputTokens
				usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			}

		case "content_block_start":
			var evt contentBlockStartEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			if evt.ContentBlock.Type == "tool_use" {
				name := evt.ContentBlock.Name
				if oauth {
					name = maybeStripToolPrefix(name)
				}
				activeTools[evt.Index] = &toolBlock{
					id:   evt.ContentBlock.ID,
					name: name,
				}
			}

		case "content_block_delta":
			var evt contentBlockDeltaEvent
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			switch evt.Delta.Type {
			case "text_delta":
				events <- llm.StreamEvent{
					Type:  llm.StreamEventDelta,
					Delta: evt.Delta.Text,
				}
			case "input_json_delta":
				if tb, ok := activeTools[evt.Index]; ok {
					tb.jsonBuf.WriteString(evt.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			var evt struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &evt); err != nil {
				continue
			}
			if tb, ok := activeTools[evt.Index]; ok {
				var args map[string]any
				if tb.jsonBuf.Len() > 0 {
					_ = json.Unmarshal([]byte(tb.jsonBuf.String()), &args)
				}
				events <- llm.StreamEvent{
					Type: llm.StreamEventToolCall,
					ToolCall: &llm.ToolCall{
						ID:        tb.id,
						Name:      tb.name,
						Arguments: args,
					},
				}
				delete(activeTools, evt.Index)
			}

		case "message_stop":
			events <- llm.StreamEvent{Type: llm.StreamEventDone, Usage: &usage}
			return

		case "error":
			var errEvt struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errEvt); err == nil {
				events <- llm.StreamEvent{
					Type:  llm.StreamEventError,
					Error: fmt.Errorf("anthropic: %s", errEvt.Error.Message),
				}
			}
			return
		}
	}
}
