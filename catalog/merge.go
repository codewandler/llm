package catalog

import (
	"fmt"
	"reflect"
	"strings"
)

func MergeCatalogFragment(dst *Catalog, frag *Fragment) error {
	if dst == nil {
		return fmt.Errorf("catalog is nil")
	}
	ensureCatalogMaps(dst)

	for _, service := range frag.Services {
		if err := mergeService(dst, service); err != nil {
			return err
		}
	}
	for _, model := range frag.Models {
		if err := mergeModel(dst, model); err != nil {
			return err
		}
	}
	for _, offering := range frag.Offerings {
		if err := mergeOffering(dst, offering); err != nil {
			return err
		}
	}
	return nil
}

func MergeResolvedFragment(dst *ResolvedCatalog, frag *Fragment) error {
	if dst == nil {
		return fmt.Errorf("resolved catalog is nil")
	}
	if err := MergeCatalogFragment(&dst.Catalog, frag); err != nil {
		return err
	}
	ensureResolvedMaps(dst)

	for _, runtime := range frag.Runtimes {
		if err := mergeRuntime(dst, runtime); err != nil {
			return err
		}
	}
	for _, access := range frag.RuntimeAccess {
		if err := mergeRuntimeAccess(dst, access); err != nil {
			return err
		}
	}
	for _, acquisition := range frag.RuntimeAcquisition {
		if err := mergeRuntimeAcquisition(dst, acquisition); err != nil {
			return err
		}
	}
	return nil
}

func mergeModel(dst *Catalog, model ModelRecord) error {
	model.Key = NormalizeKey(model.Key)
	model.Aliases = normalizeStrings(model.Aliases)
	model.InputModalities = normalizeStrings(model.InputModalities)
	model.OutputModalities = normalizeStrings(model.OutputModalities)
	model.KnowledgeCutoff = normalizeDate(model.KnowledgeCutoff)

	existing, ok := dst.Models[model.Key]
	if !ok {
		dst.Models[model.Key] = model
		return nil
	}

	// Special handling for model names: prefer non-empty, trimmed values (silent conflicts)
	if existing.Name == "" && model.Name != "" {
		existing.Name = model.Name
	} else if existing.Name != "" && model.Name != "" && existing.Name != model.Name {
		// Try trimming whitespace
		existingTrimmed := strings.TrimSpace(existing.Name)
		modelTrimmed := strings.TrimSpace(model.Name)
		if existingTrimmed == modelTrimmed {
			// They match after trimming, use the trimmed version
			existing.Name = existingTrimmed
		}
		// Otherwise, keep existing without warning (expected when sources have different naming conventions)
	}
	existing.Aliases = mergeStringSlices(existing.Aliases, model.Aliases)
	// Special handling for descriptions: prefer non-empty (silently ignore conflicts)
	if existing.Description == "" && model.Description != "" {
		existing.Description = model.Description
	}
	// Keep existing when both present (expected when sources have different description versions)

	// Special handling for modality: prefer non-empty (silently ignore conflicts)
	if existing.Modality == "" && model.Modality != "" {
		existing.Modality = model.Modality
	}
	// Keep existing when both present (expected when sources have different modality representations)

	var err error
	if existing.Tokenizer, err = mergeStringField(existing.Tokenizer, model.Tokenizer, "model.tokenizer", modelID(model.Key)); err != nil {
		return err
	}
	if existing.ExpirationDate, err = mergeDateField(existing.ExpirationDate, model.ExpirationDate, "model.expiration_date", modelID(model.Key)); err != nil {
		return err
	}
	existing.Canonical = existing.Canonical || model.Canonical
	existing.Attachment = existing.Attachment || model.Attachment
	existing.OpenWeights = existing.OpenWeights || model.OpenWeights
	existing.Capabilities = mergeCapabilities(existing.Capabilities, model.Capabilities)
	if existing.Limits, err = mergeLimits(existing.Limits, model.Limits, modelID(model.Key)); err != nil {
		return err
	}
	existing.InputModalities = mergeStringSlices(existing.InputModalities, model.InputModalities)
	existing.OutputModalities = mergeStringSlices(existing.OutputModalities, model.OutputModalities)
	if existing.KnowledgeCutoff, err = mergeDateField(existing.KnowledgeCutoff, model.KnowledgeCutoff, "model.knowledge_cutoff", modelID(model.Key)); err != nil {
		return err
	}
	if existing.LastUpdated, err = mergeDateField(existing.LastUpdated, model.LastUpdated, "model.last_updated", modelID(model.Key)); err != nil {
		return err
	}
	existing.Deprecated = existing.Deprecated || model.Deprecated
	if existing.ReferencePricing, err = mergePointerField(existing.ReferencePricing, model.ReferencePricing, "model.reference_pricing", modelID(model.Key)); err != nil {
		return err
	}
	existing.Provenance = append(existing.Provenance, model.Provenance...)
	dst.Models[model.Key] = existing
	return nil
}

func mergeService(dst *Catalog, service Service) error {
	service.ID = normalizeKeyPart(service.ID)
	service.Name = strings.TrimSpace(service.Name)
	service.Operator = normalizeKeyPart(service.Operator)

	existing, ok := dst.Services[service.ID]
	if !ok {
		dst.Services[service.ID] = service
		return nil
	}

	var err error
	if existing.Name, err = mergeStringField(existing.Name, service.Name, "service.name", service.ID); err != nil {
		return err
	}
	if existing.Kind, err = mergeStringField(existing.Kind, service.Kind, "service.kind", service.ID); err != nil {
		return err
	}
	if existing.Operator, err = mergeStringField(existing.Operator, service.Operator, "service.operator", service.ID); err != nil {
		return err
	}
	if existing.APIURL, err = mergeStringField(existing.APIURL, service.APIURL, "service.api_url", service.ID); err != nil {
		return err
	}
	if existing.DocsURL, err = mergeStringField(existing.DocsURL, service.DocsURL, "service.docs_url", service.ID); err != nil {
		return err
	}
	existing.EnvVars = mergeStringSlices(existing.EnvVars, service.EnvVars)
	if existing.Package, err = mergeStringField(existing.Package, service.Package, "service.package", service.ID); err != nil {
		return err
	}
	existing.Provenance = append(existing.Provenance, service.Provenance...)
	dst.Services[service.ID] = existing
	return nil
}

func mergeOffering(dst *Catalog, offering Offering) error {
	offering.ServiceID = normalizeKeyPart(offering.ServiceID)
	offering.WireModelID = strings.TrimSpace(offering.WireModelID)
	offering.ModelKey = NormalizeKey(offering.ModelKey)
	offering.Aliases = normalizeStrings(offering.Aliases)
	offering.APITypes = normalizeStrings(offering.APITypes)
	offering.SupportedParameters = normalizeStrings(offering.SupportedParameters)

	ref := OfferingRef{ServiceID: offering.ServiceID, WireModelID: offering.WireModelID}
	existing, ok := dst.Offerings[ref]
	if !ok {
		dst.Offerings[ref] = offering
		return nil
	}
	if existing.ModelKey != offering.ModelKey {
		return fmt.Errorf("offering %s/%s maps to conflicting model keys: %q vs %q", ref.ServiceID, ref.WireModelID, modelID(existing.ModelKey), modelID(offering.ModelKey))
	}

	var err error
	existing.Aliases = mergeStringSlices(existing.Aliases, offering.Aliases)
	existing.APITypes = mergeStringSlices(existing.APITypes, offering.APITypes)
	if existing.Pricing, err = mergePointerField(existing.Pricing, offering.Pricing, "offering.pricing", ref.ServiceID+"/"+ref.WireModelID); err != nil {
		return err
	}
	if existing.LimitsOverride, err = mergePointerField(existing.LimitsOverride, offering.LimitsOverride, "offering.limits_override", ref.ServiceID+"/"+ref.WireModelID); err != nil {
		return err
	}
	existing.SupportedParameters = mergeStringSlices(existing.SupportedParameters, offering.SupportedParameters)
	if existing.DefaultParameters, err = mergePointerField(existing.DefaultParameters, offering.DefaultParameters, "offering.default_parameters", ref.ServiceID+"/"+ref.WireModelID); err != nil {
		return err
	}
	if existing.PerRequestLimits, err = mergePointerField(existing.PerRequestLimits, offering.PerRequestLimits, "offering.per_request_limits", ref.ServiceID+"/"+ref.WireModelID); err != nil {
		return err
	}
	existing.IsModerated = existing.IsModerated || offering.IsModerated
	existing.Provenance = append(existing.Provenance, offering.Provenance...)
	dst.Offerings[ref] = existing
	return nil
}

func mergeRuntime(dst *ResolvedCatalog, runtime Runtime) error {
	runtime.ID = normalizeKeyPart(runtime.ID)
	runtime.ServiceID = normalizeKeyPart(runtime.ServiceID)
	runtime.Name = strings.TrimSpace(runtime.Name)
	runtime.Endpoint = strings.TrimSpace(runtime.Endpoint)
	runtime.Region = normalizeKeyPart(runtime.Region)
	runtime.Profile = strings.TrimSpace(runtime.Profile)

	existing, ok := dst.Runtimes[runtime.ID]
	if !ok {
		dst.Runtimes[runtime.ID] = runtime
		return nil
	}

	var err error
	if existing.ServiceID, err = mergeStringField(existing.ServiceID, runtime.ServiceID, "runtime.service_id", runtime.ID); err != nil {
		return err
	}
	if existing.Name, err = mergeStringField(existing.Name, runtime.Name, "runtime.name", runtime.ID); err != nil {
		return err
	}
	if existing.Endpoint, err = mergeStringField(existing.Endpoint, runtime.Endpoint, "runtime.endpoint", runtime.ID); err != nil {
		return err
	}
	if existing.Region, err = mergeStringField(existing.Region, runtime.Region, "runtime.region", runtime.ID); err != nil {
		return err
	}
	if existing.Profile, err = mergeStringField(existing.Profile, runtime.Profile, "runtime.profile", runtime.ID); err != nil {
		return err
	}
	if existing.Local != runtime.Local && (existing.Local || runtime.Local) {
		return fmt.Errorf("runtime.local conflict for %s", runtime.ID)
	}
	existing.Local = existing.Local || runtime.Local
	existing.Provenance = append(existing.Provenance, runtime.Provenance...)
	dst.Runtimes[runtime.ID] = existing
	return nil
}

func mergeRuntimeAccess(dst *ResolvedCatalog, access RuntimeAccess) error {
	key := RuntimeAccessKey{
		RuntimeID:   normalizeKeyPart(access.RuntimeID),
		ServiceID:   normalizeKeyPart(access.Offering.ServiceID),
		WireModelID: strings.TrimSpace(access.Offering.WireModelID),
	}
	access.RuntimeID = key.RuntimeID
	access.Offering = OfferingRef{ServiceID: key.ServiceID, WireModelID: key.WireModelID}
	access.ResolvedWireID = strings.TrimSpace(access.ResolvedWireID)
	access.Reason = strings.TrimSpace(access.Reason)

	existing, ok := dst.RuntimeAccess[key]
	if !ok {
		dst.RuntimeAccess[key] = access
		return nil
	}
	if existing.Routable != access.Routable {
		return fmt.Errorf("runtime access conflict for %s/%s on runtime %s", key.ServiceID, key.WireModelID, key.RuntimeID)
	}
	var err error
	if existing.ResolvedWireID, err = mergeStringField(existing.ResolvedWireID, access.ResolvedWireID, "runtime_access.resolved_wire_id", key.RuntimeID); err != nil {
		return err
	}
	if existing.Reason, err = mergeStringField(existing.Reason, access.Reason, "runtime_access.reason", key.RuntimeID); err != nil {
		return err
	}
	existing.Provenance = append(existing.Provenance, access.Provenance...)
	dst.RuntimeAccess[key] = existing
	return nil
}

func mergeRuntimeAcquisition(dst *ResolvedCatalog, acquisition RuntimeAcquisition) error {
	key := RuntimeAcquisitionKey{
		RuntimeID:   normalizeKeyPart(acquisition.RuntimeID),
		ServiceID:   normalizeKeyPart(acquisition.Offering.ServiceID),
		WireModelID: strings.TrimSpace(acquisition.Offering.WireModelID),
	}
	acquisition.RuntimeID = key.RuntimeID
	acquisition.Offering = OfferingRef{ServiceID: key.ServiceID, WireModelID: key.WireModelID}
	acquisition.Status = normalizeKeyPart(acquisition.Status)
	acquisition.Action = normalizeKeyPart(acquisition.Action)

	existing, ok := dst.RuntimeAcquisition[key]
	if !ok {
		dst.RuntimeAcquisition[key] = acquisition
		return nil
	}
	if existing.Known != acquisition.Known {
		return fmt.Errorf("runtime acquisition known conflict for %s/%s on runtime %s", key.ServiceID, key.WireModelID, key.RuntimeID)
	}
	if existing.Acquirable != acquisition.Acquirable {
		return fmt.Errorf("runtime acquisition acquirable conflict for %s/%s on runtime %s", key.ServiceID, key.WireModelID, key.RuntimeID)
	}
	var err error
	if existing.Status, err = mergeStringField(existing.Status, acquisition.Status, "runtime_acquisition.status", key.RuntimeID); err != nil {
		return err
	}
	if existing.Action, err = mergeStringField(existing.Action, acquisition.Action, "runtime_acquisition.action", key.RuntimeID); err != nil {
		return err
	}
	existing.Provenance = append(existing.Provenance, acquisition.Provenance...)
	dst.RuntimeAcquisition[key] = existing
	return nil
}

func mergeCapabilities(a, b Capabilities) Capabilities {
	return Capabilities{
		Reasoning:           a.Reasoning || b.Reasoning,
		ReasoningEffort:     a.ReasoningEffort || b.ReasoningEffort,
		ToolUse:             a.ToolUse || b.ToolUse,
		ParallelToolCalls:   a.ParallelToolCalls || b.ParallelToolCalls,
		StructuredOutput:    a.StructuredOutput || b.StructuredOutput,
		StructuredOutputs:   a.StructuredOutputs || b.StructuredOutputs,
		Vision:              a.Vision || b.Vision,
		Streaming:           a.Streaming || b.Streaming,
		Caching:             a.Caching || b.Caching,
		InterleavedThinking: a.InterleavedThinking || b.InterleavedThinking,
		AdaptiveThinking:    a.AdaptiveThinking || b.AdaptiveThinking,
		Temperature:         a.Temperature || b.Temperature,
		Logprobs:            a.Logprobs || b.Logprobs,
		Seed:                a.Seed || b.Seed,
		WebSearch:           a.WebSearch || b.WebSearch,
	}
}

func mergeLimits(a, b Limits, id string) (Limits, error) {
	var err error
	a.ContextWindow, err = mergeIntField(a.ContextWindow, b.ContextWindow, "limits.context_window", id)
	if err != nil {
		return Limits{}, err
	}
	a.MaxOutput, err = mergeIntField(a.MaxOutput, b.MaxOutput, "limits.max_output", id)
	if err != nil {
		return Limits{}, err
	}
	return a, nil
}

func mergeStringField[T ~string](existing, incoming T, field, id string) (T, error) {
	if existing == "" {
		return incoming, nil
	}
	if incoming == "" || existing == incoming {
		return existing, nil
	}
	return existing, fmt.Errorf("%s conflict for %s: %q vs %q", field, id, existing, incoming)
}

func mergeDateField[T ~string](existing, incoming T, field, id string) (T, error) {
	if existing == "" {
		return incoming, nil
	}
	if incoming == "" || existing == incoming {
		return existing, nil
	}
	if isComparableDate(string(existing)) && isComparableDate(string(incoming)) {
		if string(incoming) > string(existing) {
			return incoming, nil
		}
		return existing, nil
	}
	if isDatePrecisionSubset(string(existing), string(incoming)) {
		return incoming, nil
	}
	if isDatePrecisionSubset(string(incoming), string(existing)) {
		return existing, nil
	}
	return existing, fmt.Errorf("%s conflict for %s: %q vs %q", field, id, existing, incoming)
}

func mergeIntField(existing, incoming int, field, id string) (int, error) {
	if existing == 0 {
		return incoming, nil
	}
	if incoming == 0 || existing == incoming {
		return existing, nil
	}
	return existing, fmt.Errorf("%s conflict for %s: %d vs %d", field, id, existing, incoming)
}

func mergePointerField[T any](existing, incoming *T, field, id string) (*T, error) {
	if existing == nil {
		return incoming, nil
	}
	if incoming == nil || reflect.DeepEqual(existing, incoming) {
		return existing, nil
	}
	return existing, fmt.Errorf("%s conflict for %s", field, id)
}

func mergeStringSlices(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, values := range [][]string{a, b} {
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func isDatePrecisionSubset(lessSpecific, moreSpecific string) bool {
	if lessSpecific == "" || moreSpecific == "" {
		return false
	}
	if len(moreSpecific) <= len(lessSpecific) {
		return false
	}
	return strings.HasPrefix(moreSpecific, lessSpecific+"-")
}

func isComparableDate(v string) bool {
	switch len(v) {
	case 7:
		return v[4] == '-' && isDigits(v[:4]+v[5:7])
	case 10:
		return v[4] == '-' && v[7] == '-' && isDigits(v[:4]+v[5:7]+v[8:10])
	default:
		return false
	}
}

func normalizeStrings(values []string) []string {
	return mergeStringSlices(nil, values)
}

func ensureCatalogMaps(dst *Catalog) {
	if dst.Models == nil {
		dst.Models = make(map[ModelKey]ModelRecord)
	}
	if dst.Services == nil {
		dst.Services = make(map[string]Service)
	}
	if dst.Offerings == nil {
		dst.Offerings = make(map[OfferingRef]Offering)
	}
}

func ensureResolvedMaps(dst *ResolvedCatalog) {
	if dst.Runtimes == nil {
		dst.Runtimes = make(map[string]Runtime)
	}
	if dst.RuntimeAccess == nil {
		dst.RuntimeAccess = make(map[RuntimeAccessKey]RuntimeAccess)
	}
	if dst.RuntimeAcquisition == nil {
		dst.RuntimeAcquisition = make(map[RuntimeAcquisitionKey]RuntimeAcquisition)
	}
}
