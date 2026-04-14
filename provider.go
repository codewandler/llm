package llm

import (
	"context"
	"fmt"
)

type Named interface {
	Name() string
}

// Provider is the interface each LLM backend must implement.
type Provider interface {
	Named
	ModelsProvider
	ModelResolver
	Streamer
}

type baseProvider struct {
	name           string
	defaultModel   string
	streamer       Streamer
	modelResolver  ModelResolver
	modelsProvider ModelsProvider
}

func (p *baseProvider) Resolve(modelID string) (Model, error) {
	return p.modelResolver.Resolve(modelID)
}
func (p *baseProvider) Name() string   { return p.name }
func (p *baseProvider) Models() Models { return p.modelsProvider.Models() }
func (p *baseProvider) CreateStream(ctx context.Context, src Buildable) (Stream, error) {
	return p.streamer.CreateStream(ctx, src)
}

type ProviderOpt interface {
	applyProviderOption(*baseProvider)
}

type (
	OptionDefaultModel   struct{ model string }
	OptionMultiple       struct{ opts []ProviderOpt }
	OptionModelsProvider struct{ modelsProvider ModelsProvider }
	OptionStreamer       struct{ streamer Streamer }
)

func (o *OptionMultiple) applyProviderOption(p *baseProvider) {
	for _, opt := range o.opts {
		opt.applyProviderOption(p)
	}
}

func WithStreamer(streamer Streamer) *OptionStreamer { return &OptionStreamer{streamer} }
func WithModelsProvider(modelsProvider ModelsProvider) *OptionModelsProvider {
	return &OptionModelsProvider{modelsProvider}
}
func WithModels(models Models) *OptionModelsProvider {
	return WithModelsProvider(models)
}
func WithProviderOpts(opts ...ProviderOpt) *OptionMultiple { return &OptionMultiple{opts} }
func WithDefaultModel() *OptionDefaultModel                { return &OptionDefaultModel{} }

func (o *OptionDefaultModel) applyProviderOption(p *baseProvider) { p.defaultModel = o.model }
func (o *OptionModelsProvider) applyProviderOption(p *baseProvider) {
	p.modelsProvider = o.modelsProvider
}
func (o *OptionStreamer) applyProviderOption(p *baseProvider) { p.streamer = o.streamer }

func NewProvider(name string, opts ...ProviderOpt) Provider {

	p := &baseProvider{
		name:           name,
		modelsProvider: make(Models, 0),
		defaultModel:   ModelDefault,
	}
	for _, opt := range opts {
		opt.applyProviderOption(p)
	}

	if p.streamer == nil {
		p.streamer = StreamFunc(func(ctx context.Context, src Buildable) (Stream, error) {
			return nil, fmt.Errorf("no streamer configured for provider %q", p.name+"")
		})
	}

	if p.modelResolver == nil {
		p.modelResolver = ModelResolveFunc(func(modelID string) (Model, error) {
			found, ok := p.Models().ByAlias(modelID)
			if !ok {
				return Model{}, NewErrUnknownModel(p.Name(), modelID)
			}
			return found, nil
		})
	}

	return p
}
