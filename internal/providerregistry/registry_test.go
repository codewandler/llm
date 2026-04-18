package providerregistry

import (
	"context"
	"testing"

	"github.com/codewandler/llm"
	"github.com/codewandler/llm/provider/anthropic/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryHasDefaultDefinitions(t *testing.T) {
	r := New()
	for _, name := range []string{"claude", "anthropic", "bedrock", "openai", "openrouter", "minimax", "ollama", "codex", "dockermr"} {
		_, ok := r.Definition(name)
		assert.True(t, ok, name)
	}
}

func TestRegisterClaudeAccounts(t *testing.T) {
	store := testStore{keys: []string{"work", "personal"}}
	items, err := RegisterClaudeAccounts(context.Background(), store)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "personal", items[0].Name)
	assert.Equal(t, "work", items[1].Name)
	assert.Equal(t, "claude", items[0].Type)
}

type testStore struct{ keys []string }

func (s testStore) Load(context.Context, string) (*claude.Token, error) { return nil, nil }
func (s testStore) Save(context.Context, string, *claude.Token) error   { return nil }
func (s testStore) Delete(context.Context, string) error                { return nil }
func (s testStore) List(context.Context) ([]string, error)              { return s.keys, nil }

var _ claude.TokenStore = testStore{}
var _ llm.ProviderRegistry = (*Registry)(nil)
