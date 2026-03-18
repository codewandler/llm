package cmds

import (
	"context"

	"github.com/codewandler/llm/cmd/llmcli/store"
	"github.com/codewandler/llm/provider/aggregate"
	"github.com/codewandler/llm/provider/auto"
)

// createProvider builds the aggregate provider from available credentials.
func createProvider(ctx context.Context) (*aggregate.Provider, error) {
	tokenStore, err := getTokenStore()
	if err != nil {
		return nil, err
	}

	return auto.New(ctx,
		auto.WithName("llmcli"),
		auto.WithClaude(tokenStore),
	)
}

func getTokenStore() (*store.FileTokenStore, error) {
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	return store.NewFileTokenStore(dir)
}
