package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	modelcatalog "github.com/codewandler/llm/internal/modelcatalog"
	modeldb "github.com/codewandler/modeldb"
)

type Service struct {
	providers   []RegisteredProvider
	intents     map[string]IntentSelector
	preferences []PreferenceRule
	retryPolicy RetryPolicy
	wrappers    []ProviderWrapper
}

type RegisteredProvider struct {
	Name      string
	ServiceID string
	Provider  Provider
}

type IntentSelector struct {
	Model          string
	PreferredKinds []string
	PreferredNames []string
	Tags           map[string]string
}

type RetryPolicy struct {
	EnableFallback bool
}

type PreferenceRule struct {
	Intent        string
	ServiceIDs    []string
	ProviderNames []string
}

type OfferingCandidate struct {
	ServiceID string
	WireModel string
	Source    string
}

type ResolvedModelSpec struct {
	RawModel       string
	ExactName      string
	ExactServiceID string
	RequestedModel string
	Offerings      []OfferingCandidate
	FromIntent     string
	Ambiguous      bool
}

func DefaultRetryPolicy() RetryPolicy { return RetryPolicy{EnableFallback: true} }

type Executor interface {
	CreateStream(ctx context.Context, src Buildable) (Stream, error)
}

type ProviderWrapper func(RegisteredProvider, Executor) Executor

type DetectEnv struct {
	HTTPClient *http.Client
	LLMOptions []Option
}

type DetectedProvider struct {
	Name   string
	Type   string
	Params map[string]any
	Order  int
}

type ProviderRegistry interface {
	Detect(ctx context.Context, env DetectEnv, disabled map[string]bool) ([]DetectedProvider, error)
	Build(ctx context.Context, req DetectedProvider, client *http.Client, opts []Option) (Provider, error)
}

type ServiceConfig struct {
	Providers        []RegisteredProvider
	IntentAliases    map[string]IntentSelector
	Preferences      []PreferenceRule
	Wrappers         []ProviderWrapper
	RetryPolicy      RetryPolicy
	AutoDetect       bool
	DisabledTypes    map[string]bool
	HTTPClient       *http.Client
	LLMOptions       []Option
	Registry         ProviderRegistry
	DetectedRequests []DetectedProvider
}

type ServiceOption func(*ServiceConfig)

type providerExecutor struct{ provider Provider }

func (e providerExecutor) CreateStream(ctx context.Context, src Buildable) (Stream, error) {
	return e.provider.CreateStream(ctx, src)
}

func New(opts ...ServiceOption) (*Service, error) {
	cfg := ServiceConfig{RetryPolicy: DefaultRetryPolicy()}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.AutoDetect && cfg.Registry != nil {
		detected, err := cfg.Registry.Detect(context.Background(), DetectEnv{
			HTTPClient: cfg.HTTPClient,
			LLMOptions: cfg.LLMOptions,
		}, cfg.DisabledTypes)
		if err != nil {
			return nil, err
		}
		cfg.DetectedRequests = append(cfg.DetectedRequests, detected...)
	}
	for _, d := range cfg.DetectedRequests {
		if cfg.Registry == nil {
			return nil, fmt.Errorf("detected provider %q requires registry", d.Type)
		}
		provider, err := cfg.Registry.Build(context.Background(), d, cfg.HTTPClient, cfg.LLMOptions)
		if err != nil {
			return nil, err
		}
		cfg.Providers = append(cfg.Providers, RegisteredProvider{
			Name:      d.Name,
			ServiceID: d.Type,
			Provider:  provider,
		})
	}

	providers := make([]RegisteredProvider, 0, len(cfg.Providers))
	for i, p := range cfg.Providers {
		if p.Provider == nil {
			return nil, fmt.Errorf("providers[%d]: provider is nil", i)
		}
		serviceID := strings.TrimSpace(p.ServiceID)
		if serviceID == "" {
			serviceID = strings.TrimSpace(p.Provider.Name())
		}
		providers = append(providers, RegisteredProvider{
			Name:      strings.TrimSpace(p.Name),
			ServiceID: serviceID,
			Provider:  p.Provider,
		})
	}
	if len(providers) == 0 {
		return nil, ErrNoProviders
	}

	intents := make(map[string]IntentSelector, len(cfg.IntentAliases))
	for k, v := range cfg.IntentAliases {
		if strings.TrimSpace(k) == "" {
			continue
		}
		intents[k] = v
	}

	return &Service{
		providers:   providers,
		intents:     intents,
		preferences: append([]PreferenceRule(nil), cfg.Preferences...),
		retryPolicy: cfg.RetryPolicy,
		wrappers:    append([]ProviderWrapper(nil), cfg.Wrappers...),
	}, nil
}

func (s *Service) CreateStream(ctx context.Context, src Buildable) (Stream, error) {
	req, err := src.BuildRequest(ctx)
	if err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	resolvedReq := req
	resolved, err := s.resolveModelSpec(req.Model)
	if err != nil {
		return nil, err
	}
	if resolved.Ambiguous {
		return nil, ambiguousModelError(resolved)
	}
	resolvedReq.Model = resolved.RequestedModel
	candidates := s.rankCandidates(resolved)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrUnknownModel, req.Model)
	}

	var lastErr error
	for i, candidate := range candidates {
		exec := s.wrap(candidate)
		stream, err := exec.CreateStream(ctx, resolvedReq)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		if i == len(candidates)-1 || !s.shouldFallback(err) {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNoProviders
}

func (s *Service) wrap(r RegisteredProvider) Executor {
	var exec Executor = providerExecutor{provider: r.Provider}
	for i := len(s.wrappers) - 1; i >= 0; i-- {
		exec = s.wrappers[i](r, exec)
	}
	return exec
}

func (s *Service) ExplainModel(model string) (ResolvedModelSpec, []RegisteredProvider, error) {
	resolved, err := s.resolveModelSpec(model)
	if err != nil {
		return ResolvedModelSpec{}, nil, err
	}
	return resolved, s.rankCandidates(resolved), nil
}

func (s *Service) resolveModelSpec(model string) (ResolvedModelSpec, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return ResolvedModelSpec{}, ErrUnknownModel
	}
	resolved := ResolvedModelSpec{RawModel: model, RequestedModel: model}
	if intent, ok := s.intents[model]; ok && strings.TrimSpace(intent.Model) != "" {
		resolved.FromIntent = model
		resolved.RequestedModel = strings.TrimSpace(intent.Model)
	}

	name, serviceID, requestedModel := s.parseModelRef(resolved.RequestedModel)
	resolved.ExactName = name
	resolved.ExactServiceID = serviceID
	resolved.RequestedModel = requestedModel
	if requestedModel == "" {
		return ResolvedModelSpec{}, ErrUnknownModel
	}

	if name != "" {
		for _, p := range s.providers {
			if p.Name == name && (serviceID == "" || p.ServiceID == serviceID) {
				resolved.Offerings = []OfferingCandidate{{ServiceID: p.ServiceID, WireModel: requestedModel, Source: "explicit-instance"}}
				return resolved, nil
			}
		}
		return ResolvedModelSpec{}, fmt.Errorf("provider instance %q not configured", name)
	}
	if serviceID != "" {
		matched := false
		for _, p := range s.providers {
			if p.ServiceID == serviceID {
				matched = true
				break
			}
		}
		if !matched {
			return ResolvedModelSpec{}, fmt.Errorf("provider %q is not configured", serviceID)
		}
		resolved.Offerings = []OfferingCandidate{{ServiceID: serviceID, WireModel: requestedModel, Source: "explicit-service"}}
		return resolved, nil
	}

	resolved.Offerings = s.resolveOfferingCandidates(requestedModel)
	if len(resolved.Offerings) == 0 {
		for _, p := range s.providers {
			if hasProviderModel(p.Provider, requestedModel) {
				resolved.Offerings = append(resolved.Offerings, OfferingCandidate{ServiceID: p.ServiceID, WireModel: requestedModel, Source: "provider-models"})
			}
		}
	}
	if len(resolved.Offerings) == 0 {
		return ResolvedModelSpec{}, fmt.Errorf("%w: %s", ErrUnknownModel, model)
	}
	if resolved.ExactName == "" && resolved.ExactServiceID == "" && len(uniqueServiceIDs(resolved.Offerings)) > 1 {
		resolved.Ambiguous = true
	}
	return resolved, nil
}

func (s *Service) resolveOfferingCandidates(requestedModel string) []OfferingCandidate {
	cat, err := modelcatalog.LoadBuiltIn()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []OfferingCandidate
	for _, p := range s.providers {
		serviceID := modelcatalog.CanonicalProvider(p.ServiceID)
		if _, ok := cat.ResolveWireModel(serviceID, requestedModel); ok {
			key := p.ServiceID + "|" + requestedModel
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				out = append(out, OfferingCandidate{ServiceID: p.ServiceID, WireModel: requestedModel, Source: "catalog-wire"})
			}
			continue
		}
		for _, offering := range cat.OfferingsByService(serviceID) {
			if offering.WireModelID == requestedModel || containsString(offering.Aliases, requestedModel) || offeringModelHasAlias(cat, offering.ModelKey, requestedModel) {
				key := p.ServiceID + "|" + offering.WireModelID
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, OfferingCandidate{ServiceID: p.ServiceID, WireModel: offering.WireModelID, Source: "catalog-alias"})
			}
		}
	}
	return out
}

func (s *Service) rankCandidates(resolved ResolvedModelSpec) []RegisteredProvider {
	seen := make(map[string]struct{}, len(s.providers))
	candidates := make([]RegisteredProvider, 0, len(s.providers))
	for _, offering := range resolved.Offerings {
		for _, p := range s.providers {
			if p.ServiceID != offering.ServiceID {
				continue
			}
			if resolved.ExactName != "" && p.Name != resolved.ExactName {
				continue
			}
			key := providerCandidateKey(p)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, p)
		}
	}
	weights := s.preferenceWeights(resolved)
	sort.SliceStable(candidates, func(i, j int) bool {
		wi, wj := weights[providerCandidateKey(candidates[i])], weights[providerCandidateKey(candidates[j])]
		if wi != wj {
			return wi > wj
		}
		return false
	})
	return candidates
}

func (s *Service) preferenceWeights(resolved ResolvedModelSpec) map[string]int {
	weights := map[string]int{}
	for _, p := range s.providers {
		weights[providerCandidateKey(p)] = 0
	}
	for _, pref := range s.preferences {
		if pref.Intent != "" && pref.Intent != resolved.FromIntent {
			continue
		}
		for _, p := range s.providers {
			key := providerCandidateKey(p)
			for _, serviceID := range pref.ServiceIDs {
				if p.ServiceID == serviceID {
					weights[key] += 100
				}
			}
			for _, name := range pref.ProviderNames {
				if p.Name == name {
					weights[key] += 1000
				}
			}
		}
	}
	return weights
}

func uniqueServiceIDs(offerings []OfferingCandidate) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(offerings))
	for _, o := range offerings {
		if _, ok := seen[o.ServiceID]; ok {
			continue
		}
		seen[o.ServiceID] = struct{}{}
		out = append(out, o.ServiceID)
	}
	sort.Strings(out)
	return out
}

func ambiguousModelError(resolved ResolvedModelSpec) error {
	matches := make([]string, 0, len(resolved.Offerings))
	seen := map[string]struct{}{}
	for _, offering := range resolved.Offerings {
		candidate := offering.ServiceID + "/" + offering.WireModel
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		matches = append(matches, candidate)
	}
	sort.Strings(matches)
	return fmt.Errorf("model %q is ambiguous; matches: %s; use a provider-prefixed or instance-prefixed reference", resolved.RawModel, strings.Join(matches, ", "))
}

func hasProviderModel(p Provider, ref string) bool {
	_, err := p.Models().Resolve(ref)
	return err == nil
}

func providerCandidateKey(p RegisteredProvider) string {
	return p.Name + "|" + p.ServiceID + "|" + p.Provider.Name()
}

func offeringModelHasAlias(cat modelcatalog.Snapshot, modelKey modelcatalogModelKey, requestedModel string) bool {
	model, ok := cat.ModelByKey(modelKey)
	if !ok {
		return false
	}
	return containsString(model.Aliases, requestedModel)
}

type modelcatalogModelKey = modeldb.ModelKey

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func (s *Service) parseModelRef(model string) (name, serviceID, requestedModel string) {
	parts := strings.Split(model, "/")
	switch len(parts) {
	case 1:
		return "", "", parts[0]
	case 2:
		if s.hasServiceID(parts[0]) {
			return "", parts[0], parts[1]
		}
		if s.hasProviderName(parts[0]) {
			return parts[0], "", parts[1]
		}
		return "", parts[0], parts[1]
	default:
		if s.hasServiceID(parts[0]) {
			return "", parts[0], strings.Join(parts[1:], "/")
		}
		return parts[0], parts[1], strings.Join(parts[2:], "/")
	}
}

func (s *Service) hasServiceID(serviceID string) bool {
	for _, p := range s.providers {
		if p.ServiceID == serviceID {
			return true
		}
	}
	return false
}

func (s *Service) hasProviderName(name string) bool {
	if name == "" {
		return false
	}
	for _, p := range s.providers {
		if p.Name == name {
			return true
		}
	}
	return false
}

func (s *Service) shouldFallback(err error) bool {
	if !s.retryPolicy.EnableFallback {
		return false
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		if pe.StatusCode == 402 || pe.StatusCode == 429 || pe.StatusCode == 503 {
			return true
		}
		msg := strings.ToLower(pe.Error())
		for _, needle := range []string{"rate limit", "too many requests", "quota", "service unavailable", "overloaded", "temporarily unavailable", "try again"} {
			if strings.Contains(msg, needle) {
				return true
			}
		}
	}
	return false
}

func WithProvider(p Provider) ServiceOption {
	return func(c *ServiceConfig) {
		c.Providers = append(c.Providers, RegisteredProvider{Provider: p})
	}
}

func WithProviderNamed(name string, p Provider) ServiceOption {
	return func(c *ServiceConfig) {
		c.Providers = append(c.Providers, RegisteredProvider{Name: name, Provider: p})
	}
}

func WithRegisteredProvider(p RegisteredProvider) ServiceOption {
	return func(c *ServiceConfig) { c.Providers = append(c.Providers, p) }
}

func WithIntentAlias(name string, sel IntentSelector) ServiceOption {
	return func(c *ServiceConfig) {
		if c.IntentAliases == nil {
			c.IntentAliases = map[string]IntentSelector{}
		}
		c.IntentAliases[name] = sel
	}
}

func WithPreference(pref PreferenceRule) ServiceOption {
	return func(c *ServiceConfig) { c.Preferences = append(c.Preferences, pref) }
}

func WithWrapper(w ProviderWrapper) ServiceOption {
	return func(c *ServiceConfig) { c.Wrappers = append(c.Wrappers, w) }
}

func WithRetryPolicy(p RetryPolicy) ServiceOption {
	return func(c *ServiceConfig) { c.RetryPolicy = p }
}

func WithAutoDetect() ServiceOption {
	return func(c *ServiceConfig) { c.AutoDetect = true }
}

func WithoutProviderType(typeName string) ServiceOption {
	return func(c *ServiceConfig) {
		if c.DisabledTypes == nil {
			c.DisabledTypes = map[string]bool{}
		}
		c.DisabledTypes[typeName] = true
	}
}

func WithDetectedProvider(req DetectedProvider) ServiceOption {
	return func(c *ServiceConfig) { c.DetectedRequests = append(c.DetectedRequests, req) }
}

func WithoutAutoDetect() ServiceOption {
	return func(c *ServiceConfig) { c.AutoDetect = false }
}

func WithServiceHTTPClient(client *http.Client) ServiceOption {
	return func(c *ServiceConfig) { c.HTTPClient = client }
}

func WithServiceLLMOptions(opts ...Option) ServiceOption {
	return func(c *ServiceConfig) { c.LLMOptions = append(c.LLMOptions, opts...) }
}
