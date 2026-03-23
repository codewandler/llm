package bedrock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm"
)

func TestIsClaudeModel(t *testing.T) {
	assert.True(t, isClaudeModel("anthropic.claude-sonnet-4-5-20250929-v1:0"))
	assert.True(t, isClaudeModel("anthropic.claude-haiku-3-20240307-v1:0"))
	assert.False(t, isClaudeModel("amazon.nova-pro-v1:0"))
	assert.False(t, isClaudeModel("meta.llama3-70b-instruct-v1:0"))
}

func TestBuildBedrockCachePoint_NilHint(t *testing.T) {
	cp := buildBedrockCachePoint(nil, "anthropic.claude-sonnet-4-5")
	assert.Nil(t, cp)
}

func TestBuildBedrockCachePoint_DisabledHint(t *testing.T) {
	cp := buildBedrockCachePoint(&llm.CacheHint{Enabled: false}, "anthropic.claude-sonnet-4-5")
	assert.Nil(t, cp)
}

func TestBuildBedrockCachePoint_NonClaudeModel(t *testing.T) {
	cp := buildBedrockCachePoint(&llm.CacheHint{Enabled: true}, "amazon.nova-pro-v1:0")
	assert.Nil(t, cp)
}

func TestBuildBedrockCachePoint_DefaultTTL(t *testing.T) {
	cp := buildBedrockCachePoint(&llm.CacheHint{Enabled: true}, "anthropic.claude-sonnet-4-5")
	require.NotNil(t, cp)
	assert.Equal(t, types.CachePointTypeDefault, cp.Type)
	assert.Equal(t, types.CacheTTL(""), cp.Ttl)
}

func TestBuildBedrockCachePoint_OneHourTTL(t *testing.T) {
	cp := buildBedrockCachePoint(&llm.CacheHint{Enabled: true, TTL: "1h"}, "anthropic.claude-haiku-4-5")
	require.NotNil(t, cp)
	assert.Equal(t, types.CacheTTLOneHour, cp.Ttl)
}

func TestBuildRequest_CachePoint_SystemBlock(t *testing.T) {
	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: llm.Messages{
			&llm.SystemMsg{Content: "Big system", CacheHint: &llm.CacheHint{Enabled: true}},
			&llm.UserMsg{Content: "Hello"},
		},
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
	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello", CacheHint: &llm.CacheHint{Enabled: true}},
		},
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
	// Top-level CacheHint with no per-message hints: cachePoint appended to last message.
	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
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
	// If per-message hints exist, top-level hint is ignored.
	opts := llm.Request{
		Model: "anthropic.claude-sonnet-4-5-20250929-v1:0",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello", CacheHint: &llm.CacheHint{Enabled: true}},
		},
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
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	require.Len(t, input.Messages, 1)
	content := input.Messages[0].Content
	// Only the text block, no cachePoint
	require.Len(t, content, 1)
	_, isText := content[0].(*types.ContentBlockMemberText)
	assert.True(t, isText)
}

func TestBuildRequest_CachePoint_NonClaudeModel_Ignored(t *testing.T) {
	// cachePoints should NOT be injected for non-Claude models
	opts := llm.Request{
		Model: "amazon.nova-pro-v1:0",
		Messages: llm.Messages{
			&llm.UserMsg{Content: "Hello"},
		},
		CacheHint: &llm.CacheHint{Enabled: true},
	}

	input, err := buildRequest(opts)
	require.NoError(t, err)

	require.Len(t, input.Messages, 1)
	content := input.Messages[0].Content
	// No cachePoint for non-Claude model
	require.Len(t, content, 1)
}
