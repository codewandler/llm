package catalog

import "context"

type Source interface {
	ID() string
	Fetch(ctx context.Context) (*Fragment, error)
}

type Fragment struct {
	Models             []ModelRecord        `json:"models,omitempty"`
	Services           []Service            `json:"services,omitempty"`
	Offerings          []Offering           `json:"offerings,omitempty"`
	Runtimes           []Runtime            `json:"runtimes,omitempty"`
	RuntimeAccess      []RuntimeAccess      `json:"runtime_access,omitempty"`
	RuntimeAcquisition []RuntimeAcquisition `json:"runtime_acquisition,omitempty"`
}

type SourceStage string

const (
	StageBuild   SourceStage = "build"
	StageRuntime SourceStage = "runtime"
)

type AuthorityLevel string

const (
	AuthorityCanonical   AuthorityLevel = "canonical"
	AuthorityTrusted     AuthorityLevel = "trusted"
	AuthorityEnrichment  AuthorityLevel = "enrichment"
	AuthorityLocal       AuthorityLevel = "local"
	AuthorityProvisional AuthorityLevel = "provisional"
)

type RegisteredSource struct {
	Stage     SourceStage
	Authority AuthorityLevel
	Source    Source
}
