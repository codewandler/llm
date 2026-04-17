package ops_test

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/llmtest"
	"github.com/codewandler/llm/ops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeProvider returns a Provider backed by fn for stream creation.
// The adapter calls BuildRequest so that fn still receives a concrete
// llm.Request, making it easy to inspect preset fields in tests.
func makeProvider(fn func(context.Context, llm.Request) (llm.Stream, error)) llm.Provider {
	return &testProvider{
		streamer: llm.StreamFunc(func(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
			req, err := src.BuildRequest(ctx)
			if err != nil {
				return nil, err
			}
			return fn(ctx, req)
		}),
		models: llm.Models{{ID: "test-model", Aliases: []string{llm.ModelDefault}}},
	}
}

type testProvider struct {
	streamer llm.Streamer
	models   llm.Models
}

func (p *testProvider) Name() string       { return "test" }
func (p *testProvider) Models() llm.Models { return p.models }
func (p *testProvider) CreateStream(ctx context.Context, src llm.Buildable) (llm.Stream, error) {
	return p.streamer.CreateStream(ctx, src)
}

// textProvider returns a provider that emits a single text event.
func textProvider(text string) llm.Provider {
	return makeProvider(func(_ context.Context, _ llm.Request) (llm.Stream, error) {
		return llmtest.SendEvents(
			llmtest.TextEvent(text),
			llmtest.CompletedEvent(llm.StopReasonEndTurn),
		), nil
	})
}

// capturingProvider returns a provider that records the last request and emits text.
func capturingProvider(out *llm.Request, text string) llm.Provider {
	return makeProvider(func(_ context.Context, req llm.Request) (llm.Stream, error) {
		*out = req
		return llmtest.SendEvents(
			llmtest.TextEvent(text),
			llmtest.CompletedEvent(llm.StopReasonEndTurn),
		), nil
	})
}

// --- OperationFunc ---

func TestOperationFunc(t *testing.T) {
	type result struct{ Value string }
	op := ops.OperationFunc[string, result](func(_ context.Context, input string) (*result, error) {
		return &result{Value: "got: " + input}, nil
	})
	got, err := op.Run(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "got: hello", got.Value)
}

// --- NewFactory ---

func TestNewFactory(t *testing.T) {
	type params struct{ suffix string }
	type result struct{ Text string }

	f := ops.NewFactory(func(_ llm.Provider, p params) ops.Operation[string, result] {
		return ops.OperationFunc[string, result](func(_ context.Context, input string) (*result, error) {
			return &result{Text: input + p.suffix}, nil
		})
	})

	// nil provider is intentional: the OperationFunc returned by this factory
	// does not call the provider, so it never dereferences it.
	op := f.New(nil, params{suffix: "!"})
	got, err := op.Run(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "hello!", got.Text)
}

// --- Generate ---

func TestGenerate(t *testing.T) {
	tests := []struct {
		name       string
		events     []llm.Event
		want       string
		wantErrMsg string
	}{
		{
			name: "accumulates text deltas",
			events: []llm.Event{
				llmtest.TextEvent("hello "),
				llmtest.TextEvent("world"),
				llmtest.CompletedEvent(llm.StopReasonEndTurn),
			},
			want: "hello world",
		},
		{
			name:       "stream error",
			events:     []llm.Event{llmtest.ErrorEvent(llm.NewErrProviderMsg("test", "boom"))},
			wantErrMsg: "ops generate:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := makeProvider(func(_ context.Context, _ llm.Request) (llm.Stream, error) {
				return llmtest.SendEvents(tt.events...), nil
			})
			op := ops.Generate.New(provider, ops.GenerateParams{})
			got, err := op.Run(context.Background(), "say hello")
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Text)
		})
	}
}

func TestGenerate_Presets(t *testing.T) {
	var req llm.Request
	op := ops.Generate.New(capturingProvider(&req, "ok"), ops.GenerateParams{})
	_, err := op.Run(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, 0.7, req.Temperature, "default temperature should be 0.7")
}

func TestGenerate_CustomTemperature(t *testing.T) {
	var req llm.Request
	op := ops.Generate.New(capturingProvider(&req, "ok"), ops.GenerateParams{Temperature: ops.Temp(1.2)})
	_, err := op.Run(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, 1.2, req.Temperature)
}

func TestGenerate_ZeroTemperature(t *testing.T) {
	var req llm.Request
	op := ops.Generate.New(capturingProvider(&req, "ok"), ops.GenerateParams{Temperature: ops.Temp(0)})
	_, err := op.Run(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, 0.0, req.Temperature)
}

func TestGenerate_SystemPrompt(t *testing.T) {
	var req llm.Request
	op := ops.Generate.New(capturingProvider(&req, "ok"), ops.GenerateParams{SystemPrompt: "Be brief."})
	_, err := op.Run(context.Background(), "hi")
	require.NoError(t, err)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, llm.RoleSystem, req.Messages[0].Role)
}

// --- Map ---

type testPerson struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestMap_ToolMode(t *testing.T) {
	tests := []struct {
		name       string
		events     []llm.Event
		want       *testPerson
		wantErrMsg string
	}{
		{
			name: "happy path",
			events: []llm.Event{
				llmtest.ToolEvent("c1", "extract", map[string]any{"name": "Alice", "age": float64(30)}),
				llmtest.CompletedEvent(llm.StopReasonToolUse),
			},
			want: &testPerson{Name: "Alice", Age: 30},
		},
		{
			name: "no tool call",
			events: []llm.Event{
				llmtest.TextEvent("cannot extract"),
				llmtest.CompletedEvent(llm.StopReasonEndTurn),
			},
			wantErrMsg: "ops map: model did not return structured output",
		},
		{
			name:       "stream error",
			events:     []llm.Event{llmtest.ErrorEvent(llm.NewErrProviderMsg("test", "fail"))},
			wantErrMsg: "ops map:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := makeProvider(func(_ context.Context, _ llm.Request) (llm.Stream, error) {
				return llmtest.SendEvents(tt.events...), nil
			})
			op := ops.NewMap[testPerson](provider, ops.MapParams{})
			got, err := op.Run(context.Background(), "Alice is 30")
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMap_ToolMode_Presets(t *testing.T) {
	var req llm.Request
	provider := makeProvider(func(_ context.Context, r llm.Request) (llm.Stream, error) {
		req = r
		return llmtest.SendEvents(
			llmtest.ToolEvent("c1", "extract", map[string]any{"name": "X", "age": float64(1)}),
			llmtest.CompletedEvent(llm.StopReasonToolUse),
		), nil
	})
	op := ops.NewMap[testPerson](provider, ops.MapParams{})
	_, err := op.Run(context.Background(), "input")
	require.NoError(t, err)
	assert.Equal(t, 0.0, req.Temperature)
	assert.Equal(t, llm.ThinkingOff, req.Thinking)
	assert.Equal(t, llm.ToolChoiceTool{Name: "extract"}, req.ToolChoice)
}

func TestMap_JSONMode(t *testing.T) {
	provider := makeProvider(func(_ context.Context, _ llm.Request) (llm.Stream, error) {
		return llmtest.SendEvents(
			llmtest.TextEvent(`{"name":"Bob","age":25}`),
			llmtest.CompletedEvent(llm.StopReasonEndTurn),
		), nil
	})
	op := ops.NewMap[testPerson](provider, ops.MapParams{Mode: ops.MapModeJSON})
	got, err := op.Run(context.Background(), "Bob is 25")
	require.NoError(t, err)
	assert.Equal(t, &testPerson{Name: "Bob", Age: 25}, got)
}

func TestMap_JSONMode_InvalidJSON(t *testing.T) {
	op := ops.NewMap[testPerson](textProvider("not json at all"), ops.MapParams{Mode: ops.MapModeJSON})
	_, err := op.Run(context.Background(), "input")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ops map: unmarshal:")
}

func TestMap_ToolMode_WithHint(t *testing.T) {
	var req llm.Request
	provider := makeProvider(func(_ context.Context, r llm.Request) (llm.Stream, error) {
		req = r
		return llmtest.SendEvents(
			llmtest.ToolEvent("c1", "extract", map[string]any{"name": "X", "age": float64(1)}),
			llmtest.CompletedEvent(llm.StopReasonToolUse),
		), nil
	})
	op := ops.NewMap[testPerson](provider, ops.MapParams{Hint: "The document is in German."})
	_, err := op.Run(context.Background(), "input")
	require.NoError(t, err)
	require.Len(t, req.Messages, 2, "hint should produce a system message")
	assert.Equal(t, llm.RoleSystem, req.Messages[0].Role)
}

// --- Classify ---

func TestClassify(t *testing.T) {
	labels := []string{"positive", "negative", "neutral"}
	tests := []struct {
		name       string
		modelReply string
		want       string
		wantErrMsg string
	}{
		{name: "exact match", modelReply: "positive", want: "positive"},
		{name: "case-insensitive", modelReply: "NEUTRAL", want: "neutral"},
		{name: "whitespace trimmed", modelReply: "  negative  ", want: "negative"},
		{name: "unknown label", modelReply: "very positive", wantErrMsg: "ops classify: model returned unknown label"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := ops.Classify.New(textProvider(tt.modelReply), ops.ClassifyParams{Labels: labels})
			got, err := op.Run(context.Background(), "test input")
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Label)
		})
	}
}

func TestClassify_EmptyLabels(t *testing.T) {
	// Provider is never called — labels are validated before any stream is created.
	op := ops.Classify.New(textProvider("ignored"), ops.ClassifyParams{})
	_, err := op.Run(context.Background(), "anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Labels must not be empty")
}

func TestClassify_Presets(t *testing.T) {
	var req llm.Request
	provider := makeProvider(func(_ context.Context, r llm.Request) (llm.Stream, error) {
		req = r
		return llmtest.SendEvents(llmtest.TextEvent("positive"), llmtest.CompletedEvent(llm.StopReasonEndTurn)), nil
	})
	op := ops.Classify.New(provider, ops.ClassifyParams{Labels: []string{"positive"}})
	_, err := op.Run(context.Background(), "great")
	require.NoError(t, err)
	assert.Equal(t, 0.0, req.Temperature)
	assert.Equal(t, llm.ThinkingOff, req.Thinking)
}

func TestClassify_WithHint(t *testing.T) {
	var req llm.Request
	provider := makeProvider(func(_ context.Context, r llm.Request) (llm.Stream, error) {
		req = r
		return llmtest.SendEvents(llmtest.TextEvent("positive"), llmtest.CompletedEvent(llm.StopReasonEndTurn)), nil
	})
	op := ops.Classify.New(provider, ops.ClassifyParams{
		Labels: []string{"positive"},
		Hint:   "The text may use informal abbreviations.",
	})
	_, err := op.Run(context.Background(), "gr8")
	require.NoError(t, err)
	require.NotEmpty(t, req.Messages)
	// The hint should appear in the system message body.
	assert.Contains(t, req.Messages[0].Parts[0].Text, "informal abbreviations")
}

// --- Intent ---

func TestIntent(t *testing.T) {
	intents := []ops.IntentDef{
		{Name: "book_flight", Description: "Book a flight", Examples: []string{"fly to Paris"}},
		{Name: "cancel_order", Description: "Cancel an order", Examples: []string{"cancel my order"}},
	}
	tests := []struct {
		name       string
		modelReply string
		want       string
		wantErrMsg string
	}{
		{name: "matched intent", modelReply: "book_flight", want: "book_flight"},
		{name: "case-insensitive", modelReply: "CANCEL_ORDER", want: "cancel_order"},
		{name: "unknown intent", modelReply: "do_something_else", wantErrMsg: "ops intent: model returned unknown intent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := ops.Intent.New(textProvider(tt.modelReply), ops.IntentParams{Intents: intents})
			got, err := op.Run(context.Background(), "I want to fly to Berlin")
			if tt.wantErrMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Name)
		})
	}
}

func TestIntent_EmptyIntents(t *testing.T) {
	op := ops.Intent.New(textProvider("ignored"), ops.IntentParams{})
	_, err := op.Run(context.Background(), "anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Intents must not be empty")
}

func TestIntent_WithHint(t *testing.T) {
	var req llm.Request
	provider := makeProvider(func(_ context.Context, r llm.Request) (llm.Stream, error) {
		req = r
		return llmtest.SendEvents(llmtest.TextEvent("book_flight"), llmtest.CompletedEvent(llm.StopReasonEndTurn)), nil
	})
	op := ops.Intent.New(provider, ops.IntentParams{
		Intents: []ops.IntentDef{{Name: "book_flight", Description: "Book a flight"}},
		Hint:    "User speaks German.",
	})
	_, err := op.Run(context.Background(), "Ich möchte fliegen")
	require.NoError(t, err)
	require.NotEmpty(t, req.Messages)
	// The hint should appear at the end of the system prompt.
	assert.Contains(t, req.Messages[0].Parts[0].Text, "German")
}
