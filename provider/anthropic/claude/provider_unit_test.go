package claude

import (
	"encoding/json"
	"testing"

	providercore2 "github.com/codewandler/llm/internal/providercore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/agentapis/adapt"
	"github.com/codewandler/llm"
)

func TestNormalizeModel_Aliases(t *testing.T) {
	p := newClaudeModels()
	cases := []struct{ in, want string }{
		{"sonnet", ModelSonnet},
		{"Sonnet", ModelSonnet},
		{"SONNET", ModelSonnet},
		{"opus", ModelOpus},
		{"Opus", ModelOpus},
		{"haiku", ModelHaiku},
		{"Haiku", ModelHaiku},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{llm.ModelDefault, ModelSonnet},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			resolved, err := p.Resolve(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, resolved.ID)
		})
	}
}

func TestNormalizeModel_EmptyReturnsError(t *testing.T) {
	// claudeModels.Resolve does not do default-model substitution — that is
	// CreateStream's responsibility.  An empty model ID must be an error here.
	p := newClaudeModels()
	_, err := p.Resolve("")
	require.Error(t, err)
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

	body, err := buildRequestForTest(p, llm.Request{
		Model:    "claude-sonnet-4-6",
		Messages: llm.Messages{llm.User("hello")},
	})
	require.NoError(t, err)

	bodyJSON, err := json.Marshal(body)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(bodyJSON, &req))

	rawSystem, ok := req["system"]
	require.True(t, ok, "system field must be present")
	blocks, ok := rawSystem.([]any)
	require.True(t, ok, "system must be an array")
	require.GreaterOrEqual(t, len(blocks), 2, "must have at least billing + systemCore blocks")

	firstBlock := blocks[0].(map[string]any)
	assert.Equal(t, "text", firstBlock["type"])
	assert.Equal(t, billingHeader, firstBlock["text"])

	secondBlock := blocks[1].(map[string]any)
	assert.Equal(t, systemCore, secondBlock["text"])
}

func TestBuildRequest_UserSystemBlockAppended(t *testing.T) {
	p := &Provider{baseURL: defaultBaseURL, sessionID: "s"}

	body, err := buildRequestForTest(p, llm.Request{
		Model: "claude-sonnet-4-6",
		Messages: llm.Messages{
			llm.System("be helpful"),
			llm.User("hello"),
		},
	})
	require.NoError(t, err)

	bodyJSON, err := json.Marshal(body)
	require.NoError(t, err)

	var req map[string]any
	require.NoError(t, json.Unmarshal(bodyJSON, &req))

	blocks := req["system"].([]any)
	require.Equal(t, 3, len(blocks), "billing + core + user system")

	last := blocks[len(blocks)-1].(map[string]any)
	assert.Equal(t, "be helpful", last["text"])
}

func buildRequestForTest(p *Provider, llmRequest llm.Request) (*providercore2.MessagesRequest, error) {
	uReq, err := providercore2.RequestToUnified(llmRequest)
	if err != nil {
		return nil, err
	}
	msgReq, err := adapt.BuildMessagesRequest(uReq)
	if err != nil {
		return nil, err
	}
	if err := p.augmentMessagesRequest(msgReq); err != nil {
		return nil, err
	}
	return msgReq, nil
}
