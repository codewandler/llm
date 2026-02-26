package llm

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApply(t *testing.T) {
	t.Run("empty options", func(t *testing.T) {
		opts := Apply()
		assert.NotNil(t, opts)
		assert.Empty(t, opts.BaseURL)
		assert.Nil(t, opts.APIKeyFunc)
	})

	t.Run("multiple options", func(t *testing.T) {
		opts := Apply(
			WithBaseURL("https://example.com"),
			WithAPIKey("test-key"),
		)
		assert.Equal(t, "https://example.com", opts.BaseURL)
		require.NotNil(t, opts.APIKeyFunc)

		key, err := opts.APIKeyFunc(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "test-key", key)
	})

	t.Run("later options override earlier", func(t *testing.T) {
		opts := Apply(
			WithBaseURL("https://first.com"),
			WithBaseURL("https://second.com"),
		)
		assert.Equal(t, "https://second.com", opts.BaseURL)
	})
}

func TestWithBaseURL(t *testing.T) {
	opts := Apply(WithBaseURL("https://custom.api.com"))
	assert.Equal(t, "https://custom.api.com", opts.BaseURL)
}

func TestWithAPIKey(t *testing.T) {
	opts := Apply(WithAPIKey("sk-test-123"))
	require.NotNil(t, opts.APIKeyFunc)

	key, err := opts.APIKeyFunc(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "sk-test-123", key)
}

func TestWithAPIKeyFunc(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		callCount := 0
		opts := Apply(WithAPIKeyFunc(func(ctx context.Context) (string, error) {
			callCount++
			return "dynamic-key", nil
		}))

		key, err := opts.APIKeyFunc(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "dynamic-key", key)
		assert.Equal(t, 1, callCount)

		// Called again - should increment
		key, err = opts.APIKeyFunc(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "dynamic-key", key)
		assert.Equal(t, 2, callCount)
	})

	t.Run("error propagation", func(t *testing.T) {
		expectedErr := errors.New("secret not found")
		opts := Apply(WithAPIKeyFunc(func(ctx context.Context) (string, error) {
			return "", expectedErr
		}))

		_, err := opts.APIKeyFunc(context.Background())
		assert.Equal(t, expectedErr, err)
	})

	t.Run("context cancellation", func(t *testing.T) {
		opts := Apply(WithAPIKeyFunc(func(ctx context.Context) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			default:
				return "key", nil
			}
		}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := opts.APIKeyFunc(ctx)
		assert.Equal(t, context.Canceled, err)
	})
}

func TestAPIKeyFromEnv(t *testing.T) {
	// Clean up env vars after tests
	defer func() {
		os.Unsetenv("TEST_API_KEY_1")
		os.Unsetenv("TEST_API_KEY_2")
	}()

	t.Run("first candidate found", func(t *testing.T) {
		os.Setenv("TEST_API_KEY_1", "key-from-first")
		os.Setenv("TEST_API_KEY_2", "key-from-second")

		opts := Apply(APIKeyFromEnv("TEST_API_KEY_1", "TEST_API_KEY_2"))
		key, err := opts.APIKeyFunc(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "key-from-first", key)
	})

	t.Run("fallback to second candidate", func(t *testing.T) {
		os.Unsetenv("TEST_API_KEY_1")
		os.Setenv("TEST_API_KEY_2", "key-from-second")

		opts := Apply(APIKeyFromEnv("TEST_API_KEY_1", "TEST_API_KEY_2"))
		key, err := opts.APIKeyFunc(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "key-from-second", key)
	})

	t.Run("no candidates found", func(t *testing.T) {
		os.Unsetenv("TEST_API_KEY_1")
		os.Unsetenv("TEST_API_KEY_2")

		opts := Apply(APIKeyFromEnv("TEST_API_KEY_1", "TEST_API_KEY_2"))
		_, err := opts.APIKeyFunc(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "TEST_API_KEY_1")
		assert.Contains(t, err.Error(), "TEST_API_KEY_2")
	})

	t.Run("empty string is not valid", func(t *testing.T) {
		os.Setenv("TEST_API_KEY_1", "")
		os.Setenv("TEST_API_KEY_2", "actual-key")

		opts := Apply(APIKeyFromEnv("TEST_API_KEY_1", "TEST_API_KEY_2"))
		key, err := opts.APIKeyFunc(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "actual-key", key)
	})
}
