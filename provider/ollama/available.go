package ollama

import (
	"context"
	"net/http"
	"os"
	"time"
)

// EnvOllamaHost is the environment variable that overrides the Ollama base URL.
// Set it to signal that Ollama is available for auto-detection:
//
//	export OLLAMA_HOST=http://localhost:11434
const EnvOllamaHost = "OLLAMA_HOST"

// ollamaProbeTimeout is how long Available() waits for the local Ollama endpoint
// to respond before concluding it is not running.
const ollamaProbeTimeout = 200 * time.Millisecond

// Available reports whether an Ollama server is reachable.
//
// It returns true immediately when OLLAMA_HOST is set (explicit configuration).
// Otherwise it probes http://localhost:11434 with a 200 ms timeout, which is
// negligible on the loopback interface when Ollama is running locally.
func Available() bool {
	if os.Getenv(EnvOllamaHost) != "" {
		return true
	}
	return probeOllama(defaultBaseURL, ollamaProbeTimeout)
}

// probeOllama attempts a HEAD request to url within timeout.
func probeOllama(url string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	//nolint:errcheck
	resp.Body.Close()
	return true
}

// BaseURL returns the effective Ollama base URL.
// Returns the value of OLLAMA_HOST if set, otherwise http://localhost:11434.
func BaseURL() string {
	if h := os.Getenv(EnvOllamaHost); h != "" {
		return h
	}
	return defaultBaseURL
}
