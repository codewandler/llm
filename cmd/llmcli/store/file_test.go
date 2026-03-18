package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/provider/anthropic/claude"
)

func TestFileTokenStore_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	token := &claude.Token{
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-456",
		ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
	}

	// Save
	err = store.Save(ctx, "test-key", token)
	require.NoError(t, err)

	// Verify file exists with correct permissions
	path := filepath.Join(dir, "test-key.json")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "file should have 0600 permissions")

	// Load
	loaded, err := store.Load(ctx, "test-key")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, token.AccessToken, loaded.AccessToken)
	assert.Equal(t, token.RefreshToken, loaded.RefreshToken)
	assert.True(t, token.ExpiresAt.Equal(loaded.ExpiresAt), "ExpiresAt mismatch: %v != %v", token.ExpiresAt, loaded.ExpiresAt)
}

func TestFileTokenStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	token, err := store.Load(ctx, "nonexistent")

	assert.NoError(t, err, "Load should not error for missing key")
	assert.Nil(t, token, "Load should return nil for missing key")
}

func TestFileTokenStore_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	token := &claude.Token{
		AccessToken:  "to-delete",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	// Save and verify exists
	err = store.Save(ctx, "delete-me", token)
	require.NoError(t, err)

	loaded, err := store.Load(ctx, "delete-me")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	// Delete
	err = store.Delete(ctx, "delete-me")
	require.NoError(t, err)

	// Verify gone
	loaded, err = store.Load(ctx, "delete-me")
	assert.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestFileTokenStore_DeleteNotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	err = store.Delete(ctx, "never-existed")

	assert.NoError(t, err, "Delete should not error for missing key")
}

func TestFileTokenStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Save multiple tokens
	for _, key := range []string{"alpha", "beta", "gamma"} {
		token := &claude.Token{
			AccessToken:  "access-" + key,
			RefreshToken: "refresh-" + key,
			ExpiresAt:    time.Now().Add(time.Hour),
		}
		err := store.Save(ctx, key, token)
		require.NoError(t, err)
	}

	// List
	keys, err := store.List(ctx)
	require.NoError(t, err)

	assert.Len(t, keys, 3)
	assert.Contains(t, keys, "alpha")
	assert.Contains(t, keys, "beta")
	assert.Contains(t, keys, "gamma")
}

func TestFileTokenStore_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()
	keys, err := store.List(ctx)

	assert.NoError(t, err)
	assert.Empty(t, keys)
}

func TestFileTokenStore_ListIgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Save a valid token
	token := &claude.Token{
		AccessToken:  "valid",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	err = store.Save(ctx, "valid-key", token)
	require.NoError(t, err)

	// Create a non-JSON file
	err = os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0600)
	require.NoError(t, err)

	// Create a subdirectory
	err = os.Mkdir(filepath.Join(dir, "subdir"), 0700)
	require.NoError(t, err)

	// List should only return the valid key
	keys, err := store.List(ctx)
	require.NoError(t, err)

	assert.Len(t, keys, 1)
	assert.Contains(t, keys, "valid-key")
}

func TestFileTokenStore_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Write corrupted JSON
	path := filepath.Join(dir, "corrupted.json")
	err = os.WriteFile(path, []byte("not valid json {{{"), 0600)
	require.NoError(t, err)

	// Load should return error
	token, err := store.Load(ctx, "corrupted")

	assert.Error(t, err, "Load should error on corrupted JSON")
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "unmarshal token")
}

func TestFileTokenStore_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c", "credentials")

	store, err := NewFileTokenStore(nested)
	require.NoError(t, err)
	require.NotNil(t, store)

	// Verify directory was created
	info, err := os.Stat(nested)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm(), "directory should have 0700 permissions")
}

func TestFileTokenStore_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileTokenStore(dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Save initial token
	token1 := &claude.Token{
		AccessToken:  "first-access",
		RefreshToken: "first-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	err = store.Save(ctx, "overwrite", token1)
	require.NoError(t, err)

	// Save updated token with same key
	token2 := &claude.Token{
		AccessToken:  "second-access",
		RefreshToken: "second-refresh",
		ExpiresAt:    time.Now().Add(2 * time.Hour),
	}
	err = store.Save(ctx, "overwrite", token2)
	require.NoError(t, err)

	// Load should return the updated token
	loaded, err := store.Load(ctx, "overwrite")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, "second-access", loaded.AccessToken)
	assert.Equal(t, "second-refresh", loaded.RefreshToken)
}
