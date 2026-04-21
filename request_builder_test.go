package llm

import (
	"testing"

	"github.com/codewandler/llm/msg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/codewandler/llm/tool"
)

// --- Constructor ---

func TestRequestBuilder_ZeroValueConstructor(t *testing.T) {
	b := NewRequestBuilder()

	// Verify no opinionated defaults are pre-filled.
	assert.Equal(t, ThinkingAuto, b.req.Thinking)
	assert.Equal(t, EffortUnspecified, b.req.Effort)
	assert.Equal(t, float64(0), b.req.Temperature)
	assert.Nil(t, b.req.ToolChoice)
	assert.Nil(t, b.req.CacheHint)
}

// --- Fluent interface ---

func TestRequestBuilder_System_User_Roles(t *testing.T) {
	req, err := NewRequestBuilder().
		Model("test-model").
		System("You are helpful.").
		User("Hello").
		Build()

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleSystem, req.Messages[0].Role)
	assert.Equal(t, RoleUser, req.Messages[1].Role)
}

func TestRequestBuilder_System_WithCache(t *testing.T) {
	req, err := NewRequestBuilder().
		Model("test-model").
		System("You are helpful.", CacheTTL1h).
		User("Hello", CacheTTL1h).
		Build()

	require.NoError(t, err)
	require.NotNil(t, req.Messages[0].CacheHint)
	assert.True(t, req.Messages[0].CacheHint.Enabled)
	assert.Equal(t, "1h", req.Messages[0].CacheHint.TTL)
	require.NotNil(t, req.Messages[1].CacheHint)
	assert.True(t, req.Messages[1].CacheHint.Enabled)
	assert.Equal(t, "1h", req.Messages[1].CacheHint.TTL)
}

func TestRequestBuilder_System_NoCache(t *testing.T) {
	// Passing no cache opts must leave CacheHint nil.
	// Cache() with no args still calls NewCacheHint() and produces a non-nil
	// hint — the builder guards with len(cache) > 0 to preserve nil semantics.
	req, err := NewRequestBuilder().
		Model("test-model").
		System("You are helpful.").
		User("Hello").
		Build()

	require.NoError(t, err)
	assert.Nil(t, req.Messages[0].CacheHint)
	assert.Nil(t, req.Messages[1].CacheHint)
}

func TestRequestBuilder_RequestCache(t *testing.T) {
	req, err := NewRequestBuilder().
		Model("test-model").
		Cache(CacheTTL1h).
		User("Hello").
		Build()

	require.NoError(t, err)
	require.NotNil(t, req.CacheHint)
	assert.True(t, req.CacheHint.Enabled)
	assert.Equal(t, "1h", req.CacheHint.TTL)
}

func TestRequestBuilder_Append(t *testing.T) {
	assistant := Assistant("Sure, here you go.")

	req, err := NewRequestBuilder().
		Model("test-model").
		User("Hello").
		Append(assistant).
		Build()

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleAssistant, req.Messages[1].Role)
}

func TestRequestBuilder_Tools_ToolChoice(t *testing.T) {
	spec := tool.NewSpec[struct{}]("ping", "Ping the system")

	req, err := NewRequestBuilder().
		Model("test-model").
		User("Ping").
		Tools(spec.Definition()).
		ToolChoice(ToolChoiceRequired{}).
		Build()

	require.NoError(t, err)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "ping", req.Tools[0].Name)
	assert.Equal(t, ToolChoiceRequired{}, req.ToolChoice)
}

func TestRequestBuilder_Tools_Additive(t *testing.T) {
	// Two Tools() calls must accumulate, not replace.
	spec1 := tool.NewSpec[struct{}]("tool-a", "Tool A")
	spec2 := tool.NewSpec[struct{}]("tool-b", "Tool B")

	req, err := NewRequestBuilder().
		Model("test-model").
		User("go").
		Tools(spec1.Definition()).
		Tools(spec2.Definition()).
		Build()

	require.NoError(t, err)
	require.Len(t, req.Tools, 2)
	assert.Equal(t, "tool-a", req.Tools[0].Name)
	assert.Equal(t, "tool-b", req.Tools[1].Name)
}

// --- Apply ---

func TestRequestBuilder_Apply_FluentMix(t *testing.T) {
	// Apply a pre-assembled slice, then add a message fluently.
	baseOpts := []RequestOption{
		WithModel("test-model"),
		WithMaxTokens(512),
	}

	req, err := NewRequestBuilder().
		Apply(baseOpts...).
		User("Hello").
		Build()

	require.NoError(t, err)
	assert.Equal(t, "test-model", req.Model)
	assert.Equal(t, 512, req.MaxTokens)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, RoleUser, req.Messages[0].Role)
}

func TestRequestBuilder_Apply_Chainable(t *testing.T) {
	// Apply must return the builder for further chaining.
	req, err := NewRequestBuilder().
		Apply(WithModel("test-model")).
		Apply(WithUser("Hello")).
		Build()

	require.NoError(t, err)
	assert.Equal(t, "test-model", req.Model)
	require.Len(t, req.Messages, 1)
	assert.Equal(t, RoleUser, req.Messages[0].Role)
}

// --- With* option constructors ---

func TestBuildRequest_FullyOptionBased(t *testing.T) {
	// Equivalent to the fluent TestRequestBuilder_System_User_Roles.
	req, err := BuildRequest(
		WithModel("test-model"),
		WithSystem("You are helpful."),
		WithUser("Hello"),
	)

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleSystem, req.Messages[0].Role)
	assert.Equal(t, RoleUser, req.Messages[1].Role)
}

func TestWithSystem_Cache(t *testing.T) {
	req, err := BuildRequest(
		WithModel("test-model"),
		WithSystem("prompt", CacheTTL1h),
		WithUser("hi"),
	)

	require.NoError(t, err)
	require.NotNil(t, req.Messages[0].CacheHint)
	assert.Equal(t, "1h", req.Messages[0].CacheHint.TTL)
	assert.Nil(t, req.Messages[1].CacheHint) // WithUser without cache
}

func TestWithSystem_NoCache(t *testing.T) {
	// Same nil-guard as the fluent method.
	req, err := BuildRequest(
		WithModel("test-model"),
		WithSystem("prompt"),
		WithUser("hi"),
	)

	require.NoError(t, err)
	assert.Nil(t, req.Messages[0].CacheHint)
}

func TestWithCache(t *testing.T) {
	req, err := BuildRequest(
		WithModel("test-model"),
		WithCache(CacheTTL1h),
		WithUser("hi"),
	)

	require.NoError(t, err)
	require.NotNil(t, req.CacheHint)
	assert.True(t, req.CacheHint.Enabled)
	assert.Equal(t, "1h", req.CacheHint.TTL)
}

func TestWithMessages_Append(t *testing.T) {
	assistant := Assistant("Here you go.")

	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("Hello"),
		WithMessages(assistant),
	)

	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, RoleAssistant, req.Messages[1].Role)
}

func TestWithTools_WithToolChoice(t *testing.T) {
	spec := tool.NewSpec[struct{}]("search", "Search")

	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("Find it"),
		WithTools(spec.Definition()),
		WithToolChoice(ToolChoiceAuto{}),
	)

	require.NoError(t, err)
	require.Len(t, req.Tools, 1)
	assert.Equal(t, "search", req.Tools[0].Name)
	assert.Equal(t, ToolChoiceAuto{}, req.ToolChoice)
}

func TestWithTools_Additive(t *testing.T) {
	// Two WithTools() calls must accumulate, not replace.
	spec1 := tool.NewSpec[struct{}]("tool-a", "Tool A")
	spec2 := tool.NewSpec[struct{}]("tool-b", "Tool B")

	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("go"),
		WithTools(spec1.Definition()),
		WithTools(spec2.Definition()),
	)

	require.NoError(t, err)
	require.Len(t, req.Tools, 2)
	assert.Equal(t, "tool-a", req.Tools[0].Name)
	assert.Equal(t, "tool-b", req.Tools[1].Name)
}

func TestBuildRequest_PassesOptsThrough(t *testing.T) {
	// Regression: BuildRequest previously silently ignored its opts argument.
	req, err := BuildRequest(func(r *Request) {
		r.Model = "injected-model"
		r.Messages = Messages{User("hi")}
	})

	require.NoError(t, err)
	assert.Equal(t, "injected-model", req.Model)
}

func TestRequestBuilder_RequestMetaHelpers(t *testing.T) {
	req, err := NewRequestBuilder().
		Model("test-model").
		User("hello").
		EndUser("user-123").
		Metadata(map[string]any{"trace_id": "trace-1"}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, req.RequestMeta)
	assert.Equal(t, "user-123", req.RequestMeta.User)
	assert.Equal(t, "trace-1", req.RequestMeta.Metadata["trace_id"])
}

func TestWithRequestMeta_ClonesInput(t *testing.T) {
	meta := &RequestMeta{User: "user-123", Metadata: map[string]any{"trace_id": "trace-1"}}
	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("hi"),
		WithRequestMeta(meta),
	)
	require.NoError(t, err)

	meta.User = "changed"
	meta.Metadata["trace_id"] = "changed"

	require.NotNil(t, req.RequestMeta)
	assert.Equal(t, "user-123", req.RequestMeta.User)
	assert.Equal(t, "trace-1", req.RequestMeta.Metadata["trace_id"])
}

func TestWithEndUserAndMetadata(t *testing.T) {
	metadata := map[string]any{"trace_id": "trace-1"}
	req, err := BuildRequest(
		WithModel("test-model"),
		WithUser("hi"),
		WithEndUser("user-123"),
		WithMetadata(metadata),
	)
	require.NoError(t, err)

	metadata["trace_id"] = "changed"

	require.NotNil(t, req.RequestMeta)
	assert.Equal(t, "user-123", req.RequestMeta.User)
	assert.Equal(t, "trace-1", req.RequestMeta.Metadata["trace_id"])
}

func TestSynthesizeRequestCacheHint(t *testing.T) {
	hint := SynthesizeRequestCacheHint(Messages{
		System("sys"),
		msg.User("cached").Cache(msg.CacheTTL1h).Build(),
		User("later"),
	})
	require.NotNil(t, hint)
	assert.True(t, hint.Enabled)
	assert.Equal(t, "1h", hint.TTL)
}

func TestSynthesizeRequestCacheHint_None(t *testing.T) {
	assert.Nil(t, SynthesizeRequestCacheHint(Messages{System("sys"), User("hi")}))
}
