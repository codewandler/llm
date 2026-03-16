package anthropic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOAuthConfigFromPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	data := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-test","refreshToken":"sk-ant-ort01-test","expiresAt":4102444800000}}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	oauth, err := loadOAuthConfigFromPath(path)
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-oat01-test", oauth.Access)
	assert.Equal(t, "sk-ant-ort01-test", oauth.Refresh)
	assert.Equal(t, int64(4102444800000), oauth.Expires)
}

func TestOAuthConfigIsExpired(t *testing.T) {
	t.Parallel()
	expired := &OAuthConfig{Access: "x", Expires: time.Now().Add(-time.Minute).UnixMilli()}
	valid := &OAuthConfig{Access: "x", Expires: time.Now().Add(time.Hour).UnixMilli()}
	assert.True(t, expired.IsExpired())
	assert.False(t, valid.IsExpired())
}

func TestClaudeProviderNameAndModels(t *testing.T) {
	t.Parallel()
	p := NewClaudeCodeProvider()
	assert.Equal(t, claudeCodeProviderName, p.Name())
	models := p.Models()
	require.NotEmpty(t, models)

	ids := make(map[string]struct{}, len(models))
	for _, m := range models {
		ids[m.ID] = struct{}{}
	}
	_, hasSonnet := ids[claudeCodeModelSonnet]
	_, hasOpus := ids[claudeCodeModelOpus]
	_, hasHaiku := ids[claudeCodeModelHaiku]
	_, hasLatest := ids["claude-sonnet-4-5"]

	assert.True(t, hasSonnet, "expected sonnet concrete model in model list")
	assert.True(t, hasOpus, "expected opus concrete model in model list")
	assert.True(t, hasHaiku, "expected haiku concrete model in model list")
	assert.True(t, hasLatest, "expected -latest style model in model list")
}

func TestNormalizeClaudeCodeModel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, claudeCodeModelSonnet, normalizeClaudeCodeModel("sonnet"))
	assert.Equal(t, claudeCodeModelOpus, normalizeClaudeCodeModel("opus"))
	assert.Equal(t, claudeCodeModelHaiku, normalizeClaudeCodeModel("haiku"))
	assert.Equal(t, "claude-sonnet-4-6", normalizeClaudeCodeModel("claude-sonnet-4-6"))
}

func TestNewClaudeAPIRequestHeaders(t *testing.T) {
	t.Parallel()
	p := NewClaudeCodeProvider(llm.WithBaseURL("https://example.test"))
	req, err := p.newClaudeAPIRequest(context.Background(), "token-123", []byte(`{"ok":true}`))
	require.NoError(t, err)

	assert.Equal(t, "https://example.test/v1/messages?beta=true", req.URL.String())
	assert.Equal(t, "Bearer token-123", req.Header.Get("Authorization"))
	assert.Equal(t, claudeCodeBeta, req.Header.Get("Anthropic-Beta"))
	assert.Equal(t, claudeCodeUserAgent, req.Header.Get("User-Agent"))
	assert.Equal(t, "cli", req.Header.Get("X-App"))
}

func TestBuildClaudeRequestSystemBlocks(t *testing.T) {
	t.Parallel()
	p := NewClaudeCodeProvider()
	body, err := p.buildClaudeRequest(llm.StreamOptions{
		Model: "sonnet",
		Messages: llm.Messages{
			&llm.SystemMsg{Content: "custom system"},
			&llm.UserMsg{Content: "hello"},
		},
	})
	require.NoError(t, err)

	var req request
	require.NoError(t, json.Unmarshal(body, &req))
	system, ok := req.System.([]any)
	require.True(t, ok)
	require.Len(t, system, 4) // 3 CC blocks + 1 user block
	last, ok := system[3].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "custom system", last["text"])
}

func TestBuildClaudeRequestMultipleSystemMessages(t *testing.T) {
	t.Parallel()

	t.Run("multiple system messages are accumulated", func(t *testing.T) {
		p := NewClaudeCodeProvider()
		body, err := p.buildClaudeRequest(llm.StreamOptions{
			Model: "sonnet",
			Messages: llm.Messages{
				&llm.SystemMsg{Content: "first instruction"},
				&llm.SystemMsg{Content: "second instruction"},
				&llm.UserMsg{Content: "hello"},
			},
		})
		require.NoError(t, err)

		var req request
		require.NoError(t, json.Unmarshal(body, &req))
		system, ok := req.System.([]any)
		require.True(t, ok)
		require.Len(t, system, 5) // 3 CC blocks + 2 user blocks

		// First 3 are CC system blocks
		fourth, ok := system[3].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "first instruction", fourth["text"])

		fifth, ok := system[4].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "second instruction", fifth["text"])
	})

	t.Run("no user system messages results in only CC blocks", func(t *testing.T) {
		p := NewClaudeCodeProvider()
		body, err := p.buildClaudeRequest(llm.StreamOptions{
			Model: "sonnet",
			Messages: llm.Messages{
				&llm.UserMsg{Content: "hello"},
			},
		})
		require.NoError(t, err)

		var req request
		require.NoError(t, json.Unmarshal(body, &req))
		system, ok := req.System.([]any)
		require.True(t, ok)
		require.Len(t, system, 3) // Only the 3 CC system blocks
	})

	t.Run("empty system messages are filtered out", func(t *testing.T) {
		p := NewClaudeCodeProvider()
		body, err := p.buildClaudeRequest(llm.StreamOptions{
			Model: "sonnet",
			Messages: llm.Messages{
				&llm.SystemMsg{Content: "keep this"},
				&llm.SystemMsg{Content: "   "}, // whitespace only
				&llm.SystemMsg{Content: ""},    // empty
				&llm.UserMsg{Content: "hello"},
			},
		})
		require.NoError(t, err)

		var req request
		require.NoError(t, json.Unmarshal(body, &req))
		system, ok := req.System.([]any)
		require.True(t, ok)
		require.Len(t, system, 4) // 3 CC blocks + 1 user block

		last, ok := system[3].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "keep this", last["text"])
	})
}
