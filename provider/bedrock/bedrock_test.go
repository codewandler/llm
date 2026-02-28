package bedrock

import (
	"context"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCredentialsProvider implements aws.CredentialsProvider for testing.
type mockCredentialsProvider struct {
	creds aws.Credentials
	err   error
}

func (m *mockCredentialsProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	if m.err != nil {
		return aws.Credentials{}, m.err
	}
	return m.creds, nil
}

func TestNew_DefaultCredentials(t *testing.T) {
	// Without custom credentials provider, client is created immediately
	p := New()

	assert.Equal(t, "us-east-1", p.region)
	assert.Equal(t, DefaultModel, p.defaultModel)
	// Client should be created (or clientErr set if no AWS config available)
	// We can't assert client != nil because it depends on environment
}

func TestNew_WithRegion(t *testing.T) {
	p := New(WithRegion("eu-west-1"))
	assert.Equal(t, "eu-west-1", p.region)
}

func TestNew_WithDefaultModel(t *testing.T) {
	p := New(WithDefaultModel("my-model"))
	assert.Equal(t, "my-model", p.defaultModel)
}

func TestNew_WithCredentialsProvider_DefersClientCreation(t *testing.T) {
	mock := &mockCredentialsProvider{
		creds: aws.Credentials{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
		},
	}

	p := New(WithCredentialsProvider(mock))

	// Client should NOT be created yet (deferred to first use)
	assert.Nil(t, p.client)
	assert.Nil(t, p.clientErr)
	assert.NotNil(t, p.credentialsProvider)
}

func TestInitClient_LazyInitialization(t *testing.T) {
	mock := &mockCredentialsProvider{
		creds: aws.Credentials{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
		},
	}

	p := New(
		WithRegion("us-east-1"),
		WithCredentialsProvider(mock),
	)

	// Before init, client is nil
	assert.Nil(t, p.client)

	// First init should create client
	err := p.initClient(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, p.client)
	assert.Nil(t, p.clientErr)

	// Store reference to first client
	firstClient := p.client

	// Second init should be a no-op (client already exists)
	err = p.initClient(context.Background())
	require.NoError(t, err)

	// Same client instance (not recreated)
	assert.Same(t, firstClient, p.client)
}

func TestInitClient_ThreadSafety(t *testing.T) {
	mock := &mockCredentialsProvider{
		creds: aws.Credentials{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
		},
	}

	p := New(
		WithRegion("us-east-1"),
		WithCredentialsProvider(mock),
	)

	// Launch multiple goroutines to call initClient concurrently
	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	errs := make(chan error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			errs <- p.initClient(context.Background())
		}()
	}

	wg.Wait()
	close(errs)

	// All should succeed
	for err := range errs {
		assert.NoError(t, err)
	}

	// Client should be initialized exactly once
	assert.NotNil(t, p.client)
}

func TestInitClient_OnlyOnce(t *testing.T) {
	// Test that initClient is idempotent - even after error
	mock := &mockCredentialsProvider{
		creds: aws.Credentials{
			AccessKeyID:     "test-key",
			SecretAccessKey: "test-secret",
		},
	}

	p := New(
		WithRegion("us-east-1"),
		WithCredentialsProvider(mock),
	)

	// First call succeeds
	err := p.initClient(context.Background())
	require.NoError(t, err)
	client := p.client

	// Subsequent calls return immediately (no re-init)
	for i := 0; i < 5; i++ {
		err = p.initClient(context.Background())
		require.NoError(t, err)
		assert.Same(t, client, p.client)
	}
}

func TestProvider_Name(t *testing.T) {
	p := New()
	assert.Equal(t, "bedrock", p.Name())
}

func TestProvider_DefaultModel(t *testing.T) {
	p := New(WithDefaultModel("custom-model"))
	assert.Equal(t, "custom-model", p.DefaultModel())
}
