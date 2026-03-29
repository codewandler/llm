package llm

import "context"

type Streamer interface {
	CreateStream(ctx context.Context, opts Request) (Stream, error)
}

type StreamFunc func(ctx context.Context, opts Request) (Stream, error)

func (f StreamFunc) CreateStream(ctx context.Context, opts Request) (Stream, error) {
	return f(ctx, opts)
}
