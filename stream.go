package llm

import "context"

type Streamer interface {
	CreateStream(ctx context.Context, src Buildable) (Stream, error)
}

type StreamFunc func(ctx context.Context, src Buildable) (Stream, error)

func (f StreamFunc) CreateStream(ctx context.Context, src Buildable) (Stream, error) {
	return f(ctx, src)
}
