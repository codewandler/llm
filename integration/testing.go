package integration

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/bedrock"
	"github.com/codewandler/llm/provider/openai"
)

func isOpenAiAvailable() bool {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return false
	}
	p := openai.New(llm.WithAPIKey(apiKey))
	_, err := p.FetchModels(context.Background())
	if err != nil {
		return false
	}
	return true
}

// isClaudeAvailable checks if Claude Code credentials are available.
func isClaudeAvailable() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".claude", ".credentials.json"))
	return err == nil
}

// isOllamaAvailable checks if Ollama is running locally.
func isOllamaAvailable() bool {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close() // nolint:errcheck
	return resp.StatusCode == http.StatusOK
}

// isBedrockAvailable checks if AWS credentials are configured for Bedrock.
func isBedrockAvailable() bool {
	if os.Getenv("LLM_TEST_BEDROCK_ENABLED") != "1" {
		return false
	}
	// Check environment variables
	if os.Getenv(bedrock.EnvAWSAccessKeyID) != "" {
		return true
	}
	// Check for AWS config/credentials files (including SSO)
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	// Check credentials file
	credPath := filepath.Join(home, ".aws", "credentials")
	if _, err := os.Stat(credPath); err == nil {
		return true
	}
	// Check config file (may have SSO profiles)
	configPath := filepath.Join(home, ".aws", "config")
	if _, err := os.Stat(configPath); err == nil {
		return true
	}
	return false
}

// getAWSRegion returns the configured AWS region or default.
func getAWSRegion() string {
	if region := os.Getenv(bedrock.EnvAWSRegion); region != "" {
		return region
	}
	if region := os.Getenv(bedrock.EnvAWSDefaultRegion); region != "" {
		return region
	}
	return bedrock.DefaultRegion
}
