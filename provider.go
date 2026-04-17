package llm

type Named interface {
	Name() string
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Named
	ModelsProvider
	Streamer
}
