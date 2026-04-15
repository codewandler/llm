package tokencount

import "context"

// TokenCounterFunc adapts a function to the TokenCounter interface.
type TokenCounterFunc func(context.Context, TokenCountRequest) (*TokenCount, error)

// CountTokens implements TokenCounter for TokenCounterFunc.
func (f TokenCounterFunc) CountTokens(ctx context.Context, req TokenCountRequest) (*TokenCount, error) {
	return f(ctx, req)
}
