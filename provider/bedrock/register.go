package bedrock

import (
	"os"
	"path/filepath"

	"github.com/codewandler/llm"
)

// Environment variable names for AWS configuration.
const (
	EnvAWSAccessKeyID     = "AWS_ACCESS_KEY_ID"
	EnvAWSSecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	EnvAWSRegion          = "AWS_REGION"
	EnvAWSDefaultRegion   = "AWS_DEFAULT_REGION"
)

// MaybeRegister registers the Bedrock provider if AWS credentials are available.
// Credentials are detected from:
//   - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//   - Shared credentials file (~/.aws/credentials)
//   - IAM role (for EC2/ECS/Lambda) - detected at request time
//
// Region is configured from AWS_REGION or AWS_DEFAULT_REGION environment
// variables, defaulting to us-east-1 if not set.
func MaybeRegister(reg *llm.Registry) {
	if os.Getenv(EnvAWSAccessKeyID) == "" && !hasAWSCredentials() {
		return
	}

	var opts []Option
	if region := os.Getenv(EnvAWSRegion); region != "" {
		opts = append(opts, WithRegion(region))
	} else if region := os.Getenv(EnvAWSDefaultRegion); region != "" {
		opts = append(opts, WithRegion(region))
	}

	reg.Register(New(opts...))
}

// hasAWSCredentials checks if the AWS shared credentials file exists.
func hasAWSCredentials() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	credPath := filepath.Join(home, ".aws", "credentials")
	_, err = os.Stat(credPath)
	return err == nil
}
