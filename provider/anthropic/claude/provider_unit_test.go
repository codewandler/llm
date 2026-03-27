package claude

import (
	"encoding/json"
	"testing"

	"github.com/codewandler/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeModel_Aliases(t *testing.T) {
	cases := []struct{ in, want string }{
		{"sonnet", defaultModelSonnet},
		{"Sonnet", defaultModelSonnet},
		{"SONNET", defaultModelSonnet},
		{"opus", defaultModelOpus},
		{"Opus", defaultModelOpus},
		{"haiku", defaultModelHaiku},
		{"Haiku", defaultModelHaiku},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeModel(tc.in))
		})
	}
}

func TestNormalizeModel_PassThrough(t *testing.T) {
	assert.Equal(t, "claude-sonnet-4-6", normalizeModel("claude-sonnet-4-6"))
	assert.Equal(t, "", normalizeModel(""))
	assert.Equal(t, "default", normalizeModel("default"))
}

func TestStainlessOS(t *testing.T) {
	os := stainlessOS()
	assert.NotEmpty(t, os)
	valid := map[string]bool{"MacOS": true, "Windows": true, "Linux": true}
	assert.True(t, valid[os], "unexpected stainlessOS value: %q", os)
}

func TestStainlessArch(t *testing.T) {
	arch := stainlessArch()
	assert.NotEmpty(t, arch)
	valid := map[string]bool{"arm64": true, "x64": true}
	assert.True(t, valid[arch], "unexpected stainlessArch value: %q", arch)
}

func TestBuildRequest_PrependsBillingAndSystemBlocks(t *testing.T) {
	p := &Provider{
		baseURL:   defaultBaseURL,
		sessionID: "test-session",
	}

	body, err := p.buildRequest(llm.Request{
		Model:    "claude-sonnet-4-6",
		Messages: llm.Messages{llm.User("hello")},
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	rawSystem, ok := req["system"]
	require.True(t, ok, "system field must be present")
	blocks, ok := rawSystem.([]any)
	require.True(t, ok, "system must be an array")
	require.GreaterOrEqual(t, len(blocks), 3, "must have at least billing + systemCore + systemIdentity blocks")

	firstBlock := blocks[0].(map[string]any)
	assert.Equal(t, "text", firstBlock["type"])
	assert.Equal(t, billingHeader, firstBlock["text"])

	secondBlock := blocks[1].(map[string]any)
	assert.Equal(t, systemCore, secondBlock["text"])

	thirdBlock := blocks[2].(map[string]any)
	assert.Equal(t, systemIdentity, thirdBlock["text"])
}

func TestBuildRequest_UserSystemBlockAppended(t *testing.T) {
	p := &Provider{baseURL: defaultBaseURL, sessionID: "s"}

	body, err := p.buildRequest(llm.Request{
		Model: "claude-sonnet-4-6",
		Messages: llm.Messages{
			llm.System("be helpful"),
			llm.User("hello"),
		},
	})
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))

	blocks := req["system"].([]any)
	require.GreaterOrEqual(t, len(blocks), 4, "billing + core + identity + user system")

	last := blocks[len(blocks)-1].(map[string]any)
	assert.Equal(t, "be helpful", last["text"])
}
