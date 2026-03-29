package bedrock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
)

func TestBuildBedrockCachePoint(t *testing.T) {
	hint := &llm.CacheHint{
		Enabled: true,
		TTL:     "1h",
	}

	cp := buildBedrockCachePoint(hint, "anthropic.claude-sonnet-4-5-20250929-v1:0")
	require.NotNil(t, cp)
	assert.Equal(t, types.CacheTTLOneHour, cp.Ttl)
}

func TestBuildRequest_CachePoint_SystemBlock(t *testing.T) {
	sysMsg := msg.System("Big system").Build()
	sysMsg.CacheHint = &llm.CacheHint{Enabled: true}

	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: msg.BuildTranscript(
			sysMsg,
			msg.User("Hello"),
		),
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	// System should have text block + cachePoint block
	require.Len(t, input.System, 2)
	_, isText := input.System[0].(*types.SystemContentBlockMemberText)
	assert.True(t, isText, "first system block should be text")
	_, isCachePoint := input.System[1].(*types.SystemContentBlockMemberCachePoint)
	assert.True(t, isCachePoint, "second system block should be cachePoint")
}

func TestBuildRequest_CachePoint_UserMessage(t *testing.T) {
	userMsg := msg.User("Hello").Build()
	userMsg.CacheHint = &llm.CacheHint{Enabled: true}

	opts := llm.Request{
		Model:    "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: msg.BuildTranscript(userMsg),
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	require.Len(t, input.Messages, 1)
	content := input.Messages[0].Content
	require.Len(t, content, 2)

	_, isText := content[0].(*types.ContentBlockMemberText)
	assert.True(t, isText)
	_, isCachePoint := content[1].(*types.ContentBlockMemberCachePoint)
	assert.True(t, isCachePoint)
}

func TestBuildRequest_CachePoint_TopLevel_AutoMode(t *testing.T) {
	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	require.Len(t, input.Messages, 1)
	content := input.Messages[0].Content
	require.Len(t, content, 2)

	_, isText := content[0].(*types.ContentBlockMemberText)
	assert.True(t, isText)
	_, isCachePoint := content[1].(*types.ContentBlockMemberCachePoint)
	assert.True(t, isCachePoint, "top-level CacheHint should append cachePoint to last message")
}

func TestBuildRequest_CachePoint_TopLevelIgnoredWhenPerMessageHintsExist(t *testing.T) {
	userMsg := msg.User("Hello").Build()
	userMsg.CacheHint = &llm.CacheHint{Enabled: true}

	opts := llm.Request{
		Model:     "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages:  msg.BuildTranscript(userMsg),
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	require.Len(t, input.Messages, 1)
	content := input.Messages[0].Content
	// Should have exactly 2 blocks: text + cachePoint from per-message hint
	// NOT 3 (no extra cachePoint from top-level)
	require.Len(t, content, 2)
}

func TestBuildRequest_NoCacheHint_NoExtraBlocks(t *testing.T) {
	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: msg.BuildTranscript(
			msg.User("Hello"),
		),
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	require.Len(t, input.Messages, 1)
	content := input.Messages[0].Content
	// Only the text block, no cachePoint
	require.Len(t, content, 1)
}
