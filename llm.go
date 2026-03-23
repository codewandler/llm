package llm

import "context"

type Streamer interface {
	CreateStream(ctx context.Context, opts Request) (<-chan StreamEvent, error)
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Name() string
	Models() []Model
	Streamer
}

// ModelFetcher is an optional interface providers can implement to list
// models dynamically from their API instead of returning a static list.
type ModelFetcher interface {
	FetchModels(ctx context.Context) ([]Model, error)
}
