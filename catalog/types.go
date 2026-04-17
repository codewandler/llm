package catalog

import "time"

type ModelRecord struct {
	Key              ModelKey     `json:"key"`
	Name             string       `json:"name,omitempty"`
	Aliases          []string     `json:"aliases,omitempty"`
	Description      string       `json:"description,omitempty"`
	Canonical        bool         `json:"canonical,omitempty"`
	Attachment       bool         `json:"attachment,omitempty"`
	OpenWeights      bool         `json:"open_weights,omitempty"`
	Capabilities     Capabilities `json:"capabilities,omitempty"`
	Limits           Limits       `json:"limits,omitempty"`
	InputModalities  []string     `json:"input_modalities,omitempty"`
	OutputModalities []string     `json:"output_modalities,omitempty"`
	KnowledgeCutoff  string       `json:"knowledge_cutoff,omitempty"`
	ExpirationDate   string       `json:"expiration_date,omitempty"`
	LastUpdated      string       `json:"last_updated,omitempty"`
	Deprecated       bool         `json:"deprecated,omitempty"`
	InstructType     string       `json:"instruct_type,omitempty"`
	Tokenizer        string       `json:"tokenizer,omitempty"`
	Modality         string       `json:"modality,omitempty"`
	ReferencePricing *Pricing     `json:"reference_pricing,omitempty"`
	Provenance       []Provenance `json:"provenance,omitempty"`
}

type Capabilities struct {
	Reasoning           bool `json:"reasoning,omitempty"`
	ReasoningEffort     bool `json:"reasoning_effort,omitempty"`
	ToolUse             bool `json:"tool_use,omitempty"`
	ParallelToolCalls   bool `json:"parallel_tool_calls,omitempty"`
	StructuredOutput    bool `json:"structured_output,omitempty"`
	StructuredOutputs   bool `json:"structured_outputs,omitempty"`
	Vision              bool `json:"vision,omitempty"`
	Streaming           bool `json:"streaming,omitempty"`
	Caching             bool `json:"caching,omitempty"`
	InterleavedThinking bool `json:"interleaved_thinking,omitempty"`
	AdaptiveThinking    bool `json:"adaptive_thinking,omitempty"`
	Temperature         bool `json:"temperature,omitempty"`
	Logprobs            bool `json:"logprobs,omitempty"`
	Seed                bool `json:"seed,omitempty"`
	WebSearch           bool `json:"web_search,omitempty"`
}

type Limits struct {
	ContextWindow int `json:"context_window,omitempty"`
	MaxOutput     int `json:"max_output,omitempty"`
}

type DefaultParameters struct {
	Temperature       *float64 `json:"temperature,omitempty"`
	TopP              *float64 `json:"top_p,omitempty"`
	TopK              *int     `json:"top_k,omitempty"`
	FrequencyPenalty  *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty   *float64 `json:"presence_penalty,omitempty"`
	RepetitionPenalty *float64 `json:"repetition_penalty,omitempty"`
}

type PerRequestLimits struct {
	PromptTokens     float64 `json:"prompt_tokens,omitempty"`
	CompletionTokens float64 `json:"completion_tokens,omitempty"`
}

type Pricing struct {
	Input       float64 `json:"input,omitempty"`
	Output      float64 `json:"output,omitempty"`
	CachedInput float64 `json:"cached_input,omitempty"`
	CacheWrite  float64 `json:"cache_write,omitempty"`
	Reasoning   float64 `json:"reasoning,omitempty"`
	Image       float64 `json:"image,omitempty"`
	ImageToken  float64 `json:"image_token,omitempty"`
	ImageOutput float64 `json:"image_output,omitempty"`
	Audio       float64 `json:"audio,omitempty"`
	AudioOutput float64 `json:"audio_output,omitempty"`
	Request     float64 `json:"request,omitempty"`
	WebSearch   float64 `json:"web_search,omitempty"`
}

type Service struct {
	ID         string       `json:"id"`
	Name       string       `json:"name,omitempty"`
	Kind       ServiceKind  `json:"kind,omitempty"`
	Operator   string       `json:"operator,omitempty"`
	APIURL     string       `json:"api_url,omitempty"`
	DocsURL    string       `json:"docs_url,omitempty"`
	EnvVars    []string     `json:"env_vars,omitempty"`
	Package    string       `json:"package,omitempty"`
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
	ServiceID           string             `json:"service_id"`
	WireModelID         string             `json:"wire_model_id"`
	ModelKey            ModelKey           `json:"model_key"`
	Aliases             []string           `json:"aliases,omitempty"`
	SupportedParameters []string           `json:"supported_parameters,omitempty"`
	DefaultParameters   *DefaultParameters `json:"default_parameters,omitempty"`
	APITypes            []string           `json:"api_types,omitempty"`
	Pricing             *Pricing           `json:"pricing,omitempty"`
	LimitsOverride      *Limits            `json:"limits_override,omitempty"`
	PerRequestLimits    *PerRequestLimits  `json:"per_request_limits,omitempty"`
	IsModerated         bool               `json:"is_moderated,omitempty"`
	Provenance          []Provenance       `json:"provenance,omitempty"`
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
