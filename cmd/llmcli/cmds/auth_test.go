package cmds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCredentialKey_RejectsBlank(t *testing.T) {
	_, err := normalizeCredentialKey("   ")
	require.Error(t, err)
}

func TestNormalizeCredentialKey_RejectsAtLocalForLogin(t *testing.T) {
	_, err := normalizeCredentialKey(localKey)
	require.NoError(t, err)
}

func TestRunLogin_RejectsBlankKeyBeforeExternalCalls(t *testing.T) {
	err := runLogin(context.Background(), "   ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "key cannot be empty")
}
