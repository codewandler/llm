package catalog

import "context"

type SourceFunc struct {
	SourceID  string
	FetchFunc func(context.Context) (*Fragment, error)
}

func (s SourceFunc) ID() string { return s.SourceID }

func (s SourceFunc) Fetch(ctx context.Context) (*Fragment, error) {
	return s.FetchFunc(ctx)
}
