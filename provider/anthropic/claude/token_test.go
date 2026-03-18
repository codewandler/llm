package claude

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToken_IsExpired_NilToken(t *testing.T) {
	var token *Token
	assert.True(t, token.IsExpired(), "nil token should be expired")
}

func TestToken_IsExpired_EmptyAccessToken(t *testing.T) {
	token := &Token{
		AccessToken: "",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	assert.True(t, token.IsExpired(), "empty access token should be expired")
}

func TestToken_IsExpired_PastExpiry(t *testing.T) {
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(-time.Hour),
	}
	assert.True(t, token.IsExpired(), "past expiry should be expired")
}

func TestToken_IsExpired_FutureExpiry(t *testing.T) {
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	assert.False(t, token.IsExpired(), "future expiry should not be expired")
}

func TestToken_IsExpired_WithinDefaultBuffer(t *testing.T) {
	// Token expires in 20 seconds, but default buffer is 30 seconds
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(20 * time.Second),
	}
	assert.True(t, token.IsExpired(), "token expiring within 30s buffer should be expired")
}

func TestToken_IsExpired_OutsideDefaultBuffer(t *testing.T) {
	// Token expires in 60 seconds, well outside 30 second buffer
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(60 * time.Second),
	}
	assert.False(t, token.IsExpired(), "token expiring in 60s should not be expired")
}

func TestToken_IsExpiredWithBuffer_ZeroBuffer(t *testing.T) {
	// Token expires in 10 seconds
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(10 * time.Second),
	}
	assert.False(t, token.IsExpiredWithBuffer(0), "token should not be expired with zero buffer")
}

func TestToken_IsExpiredWithBuffer_LargeBuffer(t *testing.T) {
	// Token expires in 1 hour, but buffer is 2 hours
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	assert.True(t, token.IsExpiredWithBuffer(2*time.Hour), "token should be expired with 2h buffer")
}

func TestToken_IsExpiredWithBuffer_ExactBoundary(t *testing.T) {
	// Token expires in exactly 1 minute
	token := &Token{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	// With 1 minute buffer, it should be at the boundary
	// time.Now().Add(1m) is After token.ExpiresAt is false when they're equal
	// But due to timing, we check both scenarios
	result := token.IsExpiredWithBuffer(time.Minute)
	// This will be true because Add(buffer).After(ExpiresAt) where both are ~equal
	assert.True(t, result, "token at exact boundary should be considered expired")
}

func TestStaticTokenProvider_ReturnsToken(t *testing.T) {
	token := &Token{
		AccessToken:  "static-access",
		RefreshToken: "static-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	provider := NewStaticTokenProvider(token)
	ctx := context.Background()

	got, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Same(t, token, got, "should return the exact same token instance")
}

func TestStaticTokenProvider_ReturnsSameTokenRepeatedly(t *testing.T) {
	token := &Token{
		AccessToken:  "static-access",
		RefreshToken: "static-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	provider := NewStaticTokenProvider(token)
	ctx := context.Background()

	// Call multiple times
	for i := 0; i < 5; i++ {
		got, err := provider.Token(ctx)
		require.NoError(t, err)
		assert.Same(t, token, got, "call %d should return same token", i)
	}
}

func TestStaticTokenProvider_ReturnsNilToken(t *testing.T) {
	provider := NewStaticTokenProvider(nil)
	ctx := context.Background()

	got, err := provider.Token(ctx)
	require.NoError(t, err)
	assert.Nil(t, got, "should return nil token")
}

func TestStaticTokenProvider_ImplementsInterface(t *testing.T) {
	var _ TokenProvider = NewStaticTokenProvider(nil)
}
