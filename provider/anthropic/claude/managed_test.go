package claude

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTokenStore is an in-memory TokenStore for testing.
type mockTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*Token
	// For tracking calls
	loadCalls   int
	saveCalls   int
	deleteCalls int
	// For injecting errors
	loadErr   error
	saveErr   error
	deleteErr error
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{
		tokens: make(map[string]*Token),
	}
}

func (s *mockTokenStore) Load(ctx context.Context, key string) (*Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadCalls++
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return s.tokens[key], nil
}

func (s *mockTokenStore) Save(ctx context.Context, key string, token *Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveCalls++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.tokens[key] = token
	return nil
}

func (s *mockTokenStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.tokens, key)
	return nil
}

func (s *mockTokenStore) List(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys []string
	for k := range s.tokens {
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *mockTokenStore) setToken(key string, token *Token) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[key] = token
}

func TestManagedTokenProvider_Token_LoadsFromStore(t *testing.T) {
	store := newMockTokenStore()
	store.setToken("test-key", &Token{
		AccessToken:  "stored-access",
		RefreshToken: "stored-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	token, err := provider.Token(ctx)
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "stored-access", token.AccessToken)
	assert.Equal(t, 1, store.loadCalls)
}

func TestManagedTokenProvider_Token_UsesCachedToken(t *testing.T) {
	store := newMockTokenStore()
	store.setToken("test-key", &Token{
		AccessToken:  "cached-access",
		RefreshToken: "cached-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	// First call loads from store
	token1, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, store.loadCalls)

	// Second call uses cache
	token2, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, store.loadCalls, "should not hit store on second call")

	// Same token instance
	assert.Same(t, token1, token2)
}

func TestManagedTokenProvider_Token_NoTokenFound(t *testing.T) {
	store := newMockTokenStore()
	provider := NewManagedTokenProvider("missing-key", store, nil)
	ctx := context.Background()

	token, err := provider.Token(ctx)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "no token found")
	assert.Contains(t, err.Error(), "missing-key")
}

func TestManagedTokenProvider_Token_LoadError(t *testing.T) {
	store := newMockTokenStore()
	store.loadErr = errors.New("database connection failed")

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	token, err := provider.Token(ctx)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "load token")
}

func TestManagedTokenProvider_Token_RefreshesExpiredToken(t *testing.T) {
	// Setup mock refresh server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "refreshed-access",
			"refresh_token": "refreshed-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	store := newMockTokenStore()
	store.setToken("test-key", &Token{
		AccessToken:  "expired-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour), // Expired
	})

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	token, err := provider.Token(ctx)
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "refreshed-access", token.AccessToken)
	assert.Equal(t, "refreshed-refresh", token.RefreshToken)
	assert.Equal(t, 1, store.saveCalls, "should save refreshed token")
}

func TestManagedTokenProvider_Token_RefreshError(t *testing.T) {
	// Setup mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "invalid_grant",
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	store := newMockTokenStore()
	store.setToken("test-key", &Token{
		AccessToken:  "expired-access",
		RefreshToken: "invalid-refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	token, err := provider.Token(ctx)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "refresh token")
}

func TestManagedTokenProvider_Token_CallsOnRefreshed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	store := newMockTokenStore()
	store.setToken("callback-key", &Token{
		AccessToken:  "expired",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(-time.Hour),
	})

	var callbackCalled bool
	var callbackKey string
	var callbackToken *Token

	callback := func(ctx context.Context, key string, token *Token) error {
		callbackCalled = true
		callbackKey = key
		callbackToken = token
		return nil
	}

	provider := NewManagedTokenProvider("callback-key", store, callback)
	ctx := context.Background()

	_, err := provider.Token(ctx)
	require.NoError(t, err)

	assert.True(t, callbackCalled, "callback should be called")
	assert.Equal(t, "callback-key", callbackKey)
	assert.Equal(t, "new-access", callbackToken.AccessToken)
}

func TestManagedTokenProvider_Invalidate(t *testing.T) {
	store := newMockTokenStore()
	store.setToken("test-key", &Token{
		AccessToken:  "original-access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	// First call loads from store
	token1, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, store.loadCalls)

	// Update store
	store.setToken("test-key", &Token{
		AccessToken:  "updated-access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	// Second call uses cache (old value)
	token2, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, store.loadCalls)
	assert.Equal(t, "original-access", token2.AccessToken)

	// Invalidate cache
	provider.Invalidate()

	// Third call reloads from store
	token3, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, store.loadCalls)
	assert.Equal(t, "updated-access", token3.AccessToken)
	assert.NotSame(t, token1, token3)
}

func TestManagedTokenProvider_Key(t *testing.T) {
	store := newMockTokenStore()
	provider := NewManagedTokenProvider("my-key", store, nil)

	assert.Equal(t, "my-key", provider.Key())
}

func TestManagedTokenProvider_Refresh_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "force-refreshed-access",
			"refresh_token": "force-refreshed-refresh",
			"expires_in":    7200,
		})
	}))
	defer server.Close()

	oldEndpoint := tokenEndpoint
	oldClient := httpClient
	tokenEndpoint = server.URL
	httpClient = server.Client()
	defer func() {
		tokenEndpoint = oldEndpoint
		httpClient = oldClient
	}()

	store := newMockTokenStore()
	// Token is NOT expired, but we want to force refresh
	store.setToken("test-key", &Token{
		AccessToken:  "valid-access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	provider := NewManagedTokenProvider("test-key", store, nil)
	ctx := context.Background()

	token, err := provider.Refresh(ctx)
	require.NoError(t, err)
	require.NotNil(t, token)

	assert.Equal(t, "force-refreshed-access", token.AccessToken)
	assert.Equal(t, 1, store.saveCalls)

	// Subsequent Token() call should return the refreshed token from cache
	cached, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Equal(t, "force-refreshed-access", cached.AccessToken)
}

func TestManagedTokenProvider_Refresh_NoToken(t *testing.T) {
	store := newMockTokenStore()
	provider := NewManagedTokenProvider("missing", store, nil)
	ctx := context.Background()

	token, err := provider.Refresh(ctx)
	assert.Error(t, err)
	assert.Nil(t, token)
	assert.Contains(t, err.Error(), "load token for refresh")
}

func TestManagedTokenProvider_ConcurrentAccess(t *testing.T) {
	store := newMockTokenStore()
	store.setToken("concurrent", &Token{
		AccessToken:  "concurrent-access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	provider := NewManagedTokenProvider("concurrent", store, nil)
	ctx := context.Background()

	// Run multiple goroutines accessing Token() concurrently
	var wg sync.WaitGroup
	results := make(chan *Token, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := provider.Token(ctx)
			if err == nil {
				results <- token
			}
		}()
	}

	wg.Wait()
	close(results)

	// All should succeed and return the same token
	var count int
	var first *Token
	for token := range results {
		count++
		if first == nil {
			first = token
		} else {
			assert.Same(t, first, token, "all goroutines should get same cached token")
		}
	}
	assert.Equal(t, 100, count)
}

// Verify interface compliance.
var _ TokenStore = (*mockTokenStore)(nil)
var _ TokenProvider = (*ManagedTokenProvider)(nil)
var _ TokenRefresher = (*ManagedTokenProvider)(nil)
