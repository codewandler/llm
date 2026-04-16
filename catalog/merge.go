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

	var err error
	if existing.Name, err = mergeStringField(existing.Name, model.Name, "model.name", modelID(model.Key)); err != nil {
		return err
	}
	existing.Aliases = mergeStringSlices(existing.Aliases, model.Aliases)
	existing.Canonical = existing.Canonical || model.Canonical
	existing.Capabilities = mergeCapabilities(existing.Capabilities, model.Capabilities)
	if existing.Limits, err = mergeLimits(existing.Limits, model.Limits, modelID(model.Key)); err != nil {
		return err
	}
	existing.InputModalities = mergeStringSlices(existing.InputModalities, model.InputModalities)
	existing.OutputModalities = mergeStringSlices(existing.OutputModalities, model.OutputModalities)
	if existing.KnowledgeCutoff, err = mergeStringField(existing.KnowledgeCutoff, model.KnowledgeCutoff, "model.knowledge_cutoff", modelID(model.Key)); err != nil {
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
		ToolUse:             a.ToolUse || b.ToolUse,
		StructuredOutput:    a.StructuredOutput || b.StructuredOutput,
		Vision:              a.Vision || b.Vision,
		Streaming:           a.Streaming || b.Streaming,
		Caching:             a.Caching || b.Caching,
		InterleavedThinking: a.InterleavedThinking || b.InterleavedThinking,
		AdaptiveThinking:    a.AdaptiveThinking || b.AdaptiveThinking,
		Temperature:         a.Temperature || b.Temperature,
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
