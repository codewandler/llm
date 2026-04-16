package catalog

import "time"

type ModelRecord struct {
	Key              ModelKey     `json:"key"`
	Name             string       `json:"name,omitempty"`
	Aliases          []string     `json:"aliases,omitempty"`
	Canonical        bool         `json:"canonical,omitempty"`
	Capabilities     Capabilities `json:"capabilities,omitempty"`
	Limits           Limits       `json:"limits,omitempty"`
	InputModalities  []string     `json:"input_modalities,omitempty"`
	OutputModalities []string     `json:"output_modalities,omitempty"`
	KnowledgeCutoff  string       `json:"knowledge_cutoff,omitempty"`
	Deprecated       bool         `json:"deprecated,omitempty"`
	ReferencePricing *Pricing     `json:"reference_pricing,omitempty"`
	Provenance       []Provenance `json:"provenance,omitempty"`
}

type Capabilities struct {
	Reasoning           bool `json:"reasoning,omitempty"`
	ToolUse             bool `json:"tool_use,omitempty"`
	StructuredOutput    bool `json:"structured_output,omitempty"`
	Vision              bool `json:"vision,omitempty"`
	Streaming           bool `json:"streaming,omitempty"`
	Caching             bool `json:"caching,omitempty"`
	InterleavedThinking bool `json:"interleaved_thinking,omitempty"`
	AdaptiveThinking    bool `json:"adaptive_thinking,omitempty"`
	Temperature         bool `json:"temperature,omitempty"`
}

type Limits struct {
	ContextWindow int `json:"context_window,omitempty"`
	MaxOutput     int `json:"max_output,omitempty"`
}

type Pricing struct {
	Input       float64 `json:"input,omitempty"`
	Output      float64 `json:"output,omitempty"`
	CachedInput float64 `json:"cached_input,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
	Reasoning   float64 `json:"reasoning,omitempty"`
}

type Service struct {
	ID         string       `json:"id"`
	Name       string       `json:"name,omitempty"`
	Kind       ServiceKind  `json:"kind,omitempty"`
	Operator   string       `json:"operator,omitempty"`
	Provenance []Provenance `json:"provenance,omitempty"`
}

type ServiceKind string

const (
	ServiceKindDirect   ServiceKind = "direct"
	ServiceKindBroker   ServiceKind = "broker"
	ServiceKindPlatform ServiceKind = "platform"
	ServiceKindLocal    ServiceKind = "local"
)

type Offering struct {
	ServiceID      string       `json:"service_id"`
	WireModelID    string       `json:"wire_model_id"`
	ModelKey       ModelKey     `json:"model_key"`
	Aliases        []string     `json:"aliases,omitempty"`
	APITypes       []string     `json:"api_types,omitempty"`
	Pricing        *Pricing     `json:"pricing,omitempty"`
	LimitsOverride *Limits      `json:"limits_override,omitempty"`
	Provenance     []Provenance `json:"provenance,omitempty"`
}

type Runtime struct {
	ID         string       `json:"id"`
	ServiceID  string       `json:"service_id"`
	Name       string       `json:"name,omitempty"`
	Local      bool         `json:"local,omitempty"`
	Endpoint   string       `json:"endpoint,omitempty"`
	Region     string       `json:"region,omitempty"`
	Profile    string       `json:"profile,omitempty"`
	Provenance []Provenance `json:"provenance,omitempty"`
}

type OfferingRef struct {
	ServiceID   string `json:"service_id"`
	WireModelID string `json:"wire_model_id"`
}

type RuntimeAccess struct {
	RuntimeID      string       `json:"runtime_id"`
	Offering       OfferingRef  `json:"offering"`
	Routable       bool         `json:"routable,omitempty"`
	ResolvedWireID string       `json:"resolved_wire_id,omitempty"`
	Reason         string       `json:"reason,omitempty"`
	Provenance     []Provenance `json:"provenance,omitempty"`
}

type RuntimeAcquisition struct {
	RuntimeID  string       `json:"runtime_id"`
	Offering   OfferingRef  `json:"offering"`
	Known      bool         `json:"known,omitempty"`
	Acquirable bool         `json:"acquirable,omitempty"`
	Status     string       `json:"status,omitempty"`
	Action     string       `json:"action,omitempty"`
	Provenance []Provenance `json:"provenance,omitempty"`
}

type Provenance struct {
	SourceID   string    `json:"source_id,omitempty"`
	Authority  string    `json:"authority,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	RawID      string    `json:"raw_id,omitempty"`
}
