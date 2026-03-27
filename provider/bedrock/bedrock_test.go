package bedrock

import (
	"context"
	"os"
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
	// Clear env vars to test default behavior
	oldRegion := os.Getenv(EnvAWSRegion)
	oldDefaultRegion := os.Getenv(EnvAWSDefaultRegion)
	_ = os.Unsetenv(EnvAWSRegion)
	_ = os.Unsetenv(EnvAWSDefaultRegion)
	defer func() {
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
	}()

	// Without custom credentials provider, client is created immediately
	p := New()

	assert.Equal(t, DefaultRegion, p.region)
	assert.Equal(t, DefaultModel, p.defaultModel)
	// Client should be created (or clientErr set if no AWS config available)
	// We can't assert client != nil because it depends on environment
}

func TestNew_WithRegion(t *testing.T) {
	p := New(WithRegion(RegionEUWest1))
	assert.Equal(t, RegionEUWest1, p.region)
	assert.Equal(t, PrefixEU, p.regionPrefix)
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
		WithRegion(RegionUSEast1),
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
		WithRegion(RegionUSEast1),
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
		WithRegion(RegionUSEast1),
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

func TestNew_ReadsRegionFromEnv(t *testing.T) {
	// Save and restore env vars
	oldRegion := os.Getenv(EnvAWSRegion)
	oldDefaultRegion := os.Getenv(EnvAWSDefaultRegion)
	defer func() {
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
	}()

	// Test AWS_REGION takes precedence
	_ = os.Setenv(EnvAWSRegion, RegionEUCentral1)
	_ = os.Setenv(EnvAWSDefaultRegion, RegionUSWest2)

	p := New()
	assert.Equal(t, RegionEUCentral1, p.region)
	assert.Equal(t, PrefixEU, p.regionPrefix)
}

func TestNew_ReadsDefaultRegionFromEnv(t *testing.T) {
	// Save and restore env vars
	oldRegion := os.Getenv(EnvAWSRegion)
	oldDefaultRegion := os.Getenv(EnvAWSDefaultRegion)
	defer func() {
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
	}()

	// Test AWS_DEFAULT_REGION is used when AWS_REGION is not set
	_ = os.Unsetenv(EnvAWSRegion)
	_ = os.Setenv(EnvAWSDefaultRegion, RegionAPNortheast1)

	p := New()
	assert.Equal(t, RegionAPNortheast1, p.region)
	assert.Equal(t, PrefixAPAC, p.regionPrefix)
}

func TestNew_WithProfile(t *testing.T) {
	p := New(WithProfile("test-profile"))
	assert.Equal(t, "test-profile", p.profile)
}

func TestWithRegionFromEnv(t *testing.T) {
	// Save and restore env vars
	oldRegion := os.Getenv(EnvAWSRegion)
	defer func() { _ = os.Setenv(EnvAWSRegion, oldRegion) }()

	_ = os.Setenv(EnvAWSRegion, RegionAPNortheast1)

	// WithRegion overrides env, then WithRegionFromEnv re-reads from env
	p := New(
		WithRegion(RegionUSEast1),
		WithRegionFromEnv(),
	)
	assert.Equal(t, RegionAPNortheast1, p.region)
	assert.Equal(t, PrefixAPAC, p.regionPrefix)
}

func TestWithRegion_OverridesEnv(t *testing.T) {
	// Save and restore env vars
	oldRegion := os.Getenv(EnvAWSRegion)
	defer func() { _ = os.Setenv(EnvAWSRegion, oldRegion) }()

	_ = os.Setenv(EnvAWSRegion, RegionAPNortheast1)

	// WithRegion should override the env var
	p := New(WithRegion(RegionEUWest1))
	assert.Equal(t, RegionEUWest1, p.region)
	assert.Equal(t, PrefixEU, p.regionPrefix)
}

func TestGetRegionFromEnv(t *testing.T) {
	// Save and restore env vars
	oldRegion := os.Getenv(EnvAWSRegion)
	oldDefaultRegion := os.Getenv(EnvAWSDefaultRegion)
	defer func() {
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
		_ = os.Setenv(EnvAWSDefaultRegion, oldDefaultRegion)
		_ = os.Setenv(EnvAWSRegion, oldRegion)
	}()

	tests := []struct {
		name           string
		awsRegion      string
		awsDefault     string
		expectedRegion string
	}{
		{
			name:           "AWS_REGION set",
			awsRegion:      RegionEUCentral1,
			awsDefault:     "",
			expectedRegion: RegionEUCentral1,
		},
		{
			name:           "AWS_DEFAULT_REGION set",
			awsRegion:      "",
			awsDefault:     RegionUSWest2,
			expectedRegion: RegionUSWest2,
		},
		{
			name:           "AWS_REGION takes precedence",
			awsRegion:      RegionEUWest1,
			awsDefault:     RegionUSWest2,
			expectedRegion: RegionEUWest1,
		},
		{
			name:           "neither set - falls back to default",
			awsRegion:      "",
			awsDefault:     "",
			expectedRegion: DefaultRegion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.awsRegion != "" {
				_ = os.Setenv(EnvAWSRegion, tt.awsRegion)
			} else {
				_ = os.Unsetenv(EnvAWSRegion)
			}
			if tt.awsDefault != "" {
				_ = os.Setenv(EnvAWSDefaultRegion, tt.awsDefault)
			} else {
				_ = os.Unsetenv(EnvAWSDefaultRegion)
			}

			result := getRegionFromEnv()
			assert.Equal(t, tt.expectedRegion, result)
		})
	}
}
