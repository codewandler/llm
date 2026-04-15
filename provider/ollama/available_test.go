package ollama

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAvailable_WithEnvVar(t *testing.T) {
	t.Setenv(EnvOllamaHost, "http://localhost:11434")
	// Env-var fast path: returns true immediately without probing.
	assert.True(t, Available())
}

func TestAvailable_DoesNotHang(t *testing.T) {
	t.Setenv(EnvOllamaHost, "")
	// Result is environment-dependent (true if Ollama is running locally).
	// Only assert that the probe respects the timeout and returns promptly.
	done := make(chan struct{})
	go func() { Available(); close(done) }()
	select {
	case <-done:
		// OK
	case <-time.After(ollamaProbeTimeout * 3):
		t.Fatalf("Available() did not return within %v", ollamaProbeTimeout*3)
	}
}

func TestBaseURL_Default(t *testing.T) {
	t.Setenv(EnvOllamaHost, "")
	assert.Equal(t, "http://localhost:11434", BaseURL())
}

func TestBaseURL_FromEnv(t *testing.T) {
	t.Setenv(EnvOllamaHost, "http://remote:11434")
	assert.Equal(t, "http://remote:11434", BaseURL())
}
