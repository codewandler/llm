package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalTokenProviderAvailable(t *testing.T) {
	// This test depends on the local environment
	// Just verify it doesn't panic
	_ = LocalTokenProviderAvailable()
}

func TestNewLocalTokenStore_FileNotFound(t *testing.T) {
	_, err := NewLocalTokenStoreWithPath("/nonexistent/path/.credentials.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLocalTokenStore_Load(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	expiresAt := time.Now().Add(1 * time.Hour).UnixMilli()
	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "test-access-token",
			"refreshToken": "test-refresh-token",
			"expiresAt":    expiresAt,
		},
	}
	data, err := json.Marshal(creds)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	token, err := store.Load(context.Background(), "any-key")
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.Equal(t, "test-access-token", token.AccessToken)
	assert.Equal(t, "test-refresh-token", token.RefreshToken)
	assert.False(t, token.IsExpired())
}

func TestLocalTokenStore_Load_MissingClaudeAiOauth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := map[string]any{"otherKey": "value"}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	token, err := store.Load(context.Background(), "key")
	require.NoError(t, err)
	assert.Nil(t, token) // No token found, not an error
}

func TestLocalTokenStore_Load_EmptyAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "",
			"refreshToken": "test-refresh-token",
			"expiresAt":    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	token, err := store.Load(context.Background(), "key")
	require.NoError(t, err)
	assert.Nil(t, token) // Empty token is treated as no token
}

func TestLocalTokenStore_Save(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	// Write initial credentials
	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "original-token",
			"refreshToken": "original-refresh",
			"expiresAt":    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	// Save a new token
	newToken := &Token{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	err = store.Save(context.Background(), "key", newToken)
	require.NoError(t, err)

	// Read back and verify
	data, err = os.ReadFile(path)
	require.NoError(t, err)

	var saved map[string]any
	require.NoError(t, json.Unmarshal(data, &saved))

	oauth := saved["claudeAiOauth"].(map[string]any)
	assert.Equal(t, "new-access-token", oauth["accessToken"])
	assert.Equal(t, "new-refresh-token", oauth["refreshToken"])
}

func TestLocalTokenStore_Save_PreservesUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	// Write initial credentials with extra fields (like Claude Code might store)
	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "original-token",
			"refreshToken":     "original-refresh",
			"expiresAt":        time.Now().Add(1 * time.Hour).UnixMilli(),
			"scopes":           []string{"user:inference", "org:create_api_key"},
			"subscriptionType": "team",
			"someOtherField":   12345,
		},
		"otherTopLevelKey": "should-be-preserved",
		"anotherKey": map[string]any{
			"nested": "data",
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	// Save a new token
	newToken := &Token{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	err = store.Save(context.Background(), "key", newToken)
	require.NoError(t, err)

	// Read back and verify ALL fields are preserved
	data, err = os.ReadFile(path)
	require.NoError(t, err)

	var saved map[string]any
	require.NoError(t, json.Unmarshal(data, &saved))

	// Token fields should be updated
	oauth := saved["claudeAiOauth"].(map[string]any)
	assert.Equal(t, "new-access-token", oauth["accessToken"])
	assert.Equal(t, "new-refresh-token", oauth["refreshToken"])

	// Unknown fields within claudeAiOauth should be preserved
	assert.Equal(t, []any{"user:inference", "org:create_api_key"}, oauth["scopes"])
	assert.Equal(t, "team", oauth["subscriptionType"])
	assert.Equal(t, float64(12345), oauth["someOtherField"]) // JSON numbers are float64

	// Top-level unknown fields should be preserved
	assert.Equal(t, "should-be-preserved", saved["otherTopLevelKey"])
	anotherKey := saved["anotherKey"].(map[string]any)
	assert.Equal(t, "data", anotherKey["nested"])
}

func TestLocalTokenStore_Save_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	// Write initial credentials
	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "original-token",
			"refreshToken": "original-refresh",
			"expiresAt":    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	// Save a new token
	newToken := &Token{
		AccessToken:  "new-access-token",
		RefreshToken: "new-refresh-token",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	err = store.Save(context.Background(), "key", newToken)
	require.NoError(t, err)

	// Verify temp file was cleaned up
	tmpPath := path + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "temp file should be removed after successful save")
}

func TestLocalTokenStore_List(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "test-token",
			"refreshToken": "test-refresh",
			"expiresAt":    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	keys, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"default"}, keys)
}

func TestLocalTokenStore_List_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	// No claudeAiOauth
	creds := map[string]any{"otherKey": "value"}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	keys, err := store.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestLocalTokenStore_Delete_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "test-token",
			"refreshToken": "test-refresh",
			"expiresAt":    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	store, err := NewLocalTokenStoreWithPath(path)
	require.NoError(t, err)

	// Delete should be no-op
	err = store.Delete(context.Background(), "key")
	require.NoError(t, err)

	// Token should still be there
	token, err := store.Load(context.Background(), "key")
	require.NoError(t, err)
	assert.NotNil(t, token)
}

func TestNewLocalTokenProvider_ReturnsManagedProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")

	creds := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  "test-token",
			"refreshToken": "test-refresh",
			"expiresAt":    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
	}
	data, _ := json.Marshal(creds)
	_ = os.WriteFile(path, data, 0600)

	provider, err := NewLocalTokenProviderWithPath(path)
	require.NoError(t, err)

	// Should be a ManagedTokenProvider
	assert.IsType(t, &ManagedTokenProvider{}, provider)

	// Should be able to get token
	token, err := provider.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-token", token.AccessToken)
}

func TestLocalTokenStore_ImplementsTokenStore(t *testing.T) {
	var _ TokenStore = (*LocalTokenStore)(nil)
}
