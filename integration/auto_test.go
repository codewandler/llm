package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/msg"
	"github.com/codewandler/llm/provider/auto"
	"github.com/codewandler/llm/tool"
	"github.com/stretchr/testify/require"
)

func TestProvider_Auto(t *testing.T) {
	p, err := auto.New(t.Context(), auto.WithClaudeLocal())
	require.NoError(t, err)

	var history = msg.BuildTranscript()

	type addParams struct {
		A int
		B int
	}

	calculator := tool.NewHandler[addParams, int]("add_numbers", func(ctx context.Context, in addParams) (*int, error) {
		r := in.A + in.B
		return &r, nil
	})

	turn := func(userMsg string) {
		history = history.Append(msg.User(userMsg))

		ch, err := p.CreateStream(t.Context(), llm.Request{
			Model:      "claude/haiku",
			Messages:   history,
			ToolChoice: llm.ToolChoiceAuto{},
			Tools: []tool.Definition{
				tool.DefinitionFor[addParams]("add_numbers", "add two numbers"),
			},
		})

		var pe *llm.ProviderError
		if errors.As(err, &pe) {
			println("API Error Body: ", pe.ResponseBody)
		}

		require.NoError(t, err)
		require.NotNil(t, ch)

		res := llm.NewEventProcessor(t.Context(), ch).
			HandleTool(calculator).
			Result()

		require.NoError(t, res.Error())
		require.NotNil(t, ch)

		d, _ := json.MarshalIndent(res, "", "  ")
		t.Log("OUT", string(d))

		next := res.Next()
		require.NoError(t, next.Validate())

		history = history.Append(next)
	}

	turn("hi, what it 1+1")
	turn("okay cool, thanks")

}
