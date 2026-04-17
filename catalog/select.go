package catalog

import (
	"fmt"
	"sort"
	"strings"
)

type ModelSelector struct {
	Name        string
	Version     string
	ReleaseDate string
}

type ModelSelection struct {
	Selector  ModelSelector
	Model     ModelRecord
	Offerings []ServiceOffering
}

type ServiceOffering struct {
	Service  Service
	Model    ModelRecord
	Offering Offering
}

type ModelSelectorNotFoundError struct {
	Selector ModelSelector
}

func (e *ModelSelectorNotFoundError) Error() string {
	return fmt.Sprintf("no catalog model matches name=%q version=%q release_date=%q", e.Selector.Name, e.Selector.Version, e.Selector.ReleaseDate)
}

type AmbiguousModelSelectorError struct {
	Selector   ModelSelector
	Candidates []ModelRecord
}

func (e *AmbiguousModelSelectorError) Error() string {
	return fmt.Sprintf("ambiguous catalog model selector name=%q version=%q release_date=%q (%d candidates)", e.Selector.Name, e.Selector.Version, e.Selector.ReleaseDate, len(e.Candidates))
}

func ParseModelSelector(name, version string) (ModelSelector, error) {
	selector := normalizeModelSelector(ModelSelector{Name: name, Version: version})
	if selector.Name == "" {
		return ModelSelector{}, fmt.Errorf("model name is required")
	}
	return selector, nil
}

func (c Catalog) SelectModel(sel ModelSelector) (ModelRecord, error) {
	sel = normalizeModelSelector(sel)
	if sel.Name == "" {
		return ModelRecord{}, fmt.Errorf("model name is required")
	}

	candidates := make([]ModelRecord, 0)
	for _, model := range c.Models {
		if !matchesModelSelector(model, sel) {
			continue
		}
		candidates = append(candidates, model)
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := formatModelID(candidates[i].Key)
		right := formatModelID(candidates[j].Key)
		if left != right {
			return left < right
		}
		return candidates[i].Name < candidates[j].Name
	})

	switch len(candidates) {
	case 0:
		return ModelRecord{}, &ModelSelectorNotFoundError{Selector: sel}
	case 1:
		return candidates[0], nil
	default:
		return ModelRecord{}, &AmbiguousModelSelectorError{Selector: sel, Candidates: candidates}
	}
}

func (c Catalog) SelectOfferingsByModel(sel ModelSelector) (ModelSelection, error) {
	sel = normalizeModelSelector(sel)
	model, err := c.SelectModel(sel)
	if err != nil {
		return ModelSelection{}, err
	}

	selected := make(map[string]ServiceOffering)
	targetLine := LineKey(model.Key)
	for _, offering := range c.Offerings {
		if LineKey(offering.ModelKey) != targetLine {
			continue
		}
		service, ok := c.Services[offering.ServiceID]
		if !ok {
			continue
		}
		candidate := ServiceOffering{Service: service, Model: model, Offering: offering}
		current, ok := selected[offering.ServiceID]
		if !ok || preferServiceOffering(sel, candidate, current) {
			selected[offering.ServiceID] = candidate
		}
	}

	services := make([]string, 0, len(selected))
	for serviceID := range selected {
		services = append(services, serviceID)
	}
	sort.Strings(services)

	selection := ModelSelection{
		Selector:  sel,
		Model:     model,
		Offerings: make([]ServiceOffering, 0, len(services)),
	}
	for _, serviceID := range services {
		selection.Offerings = append(selection.Offerings, selected[serviceID])
	}
	return selection, nil
}

func normalizeModelSelector(sel ModelSelector) ModelSelector {
	sel.Name = normalizeKeyPart(sel.Name)
	sel.Version = normalizeKeyPart(sel.Version)
	sel.ReleaseDate = normalizeDate(sel.ReleaseDate)
	return sel
}

func matchesModelSelector(model ModelRecord, sel ModelSelector) bool {
	key := NormalizeKey(model.Key)
	if sel.Version != "" && key.Version != sel.Version {
		return false
	}
	if sel.ReleaseDate != "" && key.ReleaseDate != sel.ReleaseDate {
		return false
	}
	if sel.Name == "" {
		return true
	}

	for _, candidate := range candidateModelNames(model) {
		if candidate == sel.Name {
			return true
		}
	}
	return false
}

func candidateModelNames(model ModelRecord) []string {
	key := NormalizeKey(model.Key)
	seen := make(map[string]struct{})
	ordered := make([]string, 0, len(model.Aliases)+6)
	add := func(value string) {
		value = normalizeKeyPart(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		ordered = append(ordered, value)
	}

	add(key.Creator)
	add(key.Family)
	add(key.Series)
	add(key.Variant)
	if key.Family != "" && key.Series != "" {
		add(key.Family + "-" + key.Series)
	}
	if key.Creator != "" && key.Family != "" {
		add(key.Creator + "-" + key.Family)
	}
	if key.Creator != "" && key.Family != "" && key.Series != "" {
		add(key.Creator + "-" + key.Family + "-" + key.Series)
	}
	add(model.Name)
	for _, alias := range model.Aliases {
		add(alias)
	}
	return ordered
}

func preferServiceOffering(sel ModelSelector, candidate, current ServiceOffering) bool {
	candidateHit := selectorMatchesOffering(sel, candidate.Offering)
	currentHit := selectorMatchesOffering(sel, current.Offering)
	if candidateHit != currentHit {
		return candidateHit
	}

	candidateDated := offeringUsesDatedWireID(candidate.Offering)
	currentDated := offeringUsesDatedWireID(current.Offering)
	if candidateDated != currentDated {
		return !candidateDated
	}

	if len(candidate.Offering.Aliases) != len(current.Offering.Aliases) {
		return len(candidate.Offering.Aliases) > len(current.Offering.Aliases)
	}

	return candidate.Offering.WireModelID < current.Offering.WireModelID
}

func selectorMatchesOffering(sel ModelSelector, offering Offering) bool {
	if sel.Name == "" {
		return false
	}
	if normalizeKeyPart(offering.WireModelID) == sel.Name {
		return true
	}
	for _, alias := range offering.Aliases {
		if normalizeKeyPart(alias) == sel.Name {
			return true
		}
	}
	return false
}

func offeringUsesDatedWireID(offering Offering) bool {
	releaseDate := normalizeDate(offering.ModelKey.ReleaseDate)
	if releaseDate == "" {
		return false
	}
	wireID := strings.ToLower(offering.WireModelID)
	compactDate := strings.ReplaceAll(releaseDate, "-", "")
	return strings.Contains(wireID, releaseDate) || strings.Contains(wireID, compactDate)
}

func formatModelID(key ModelKey) string {
	if releaseID := ReleaseID(key); releaseID != "" {
		return releaseID
	}
	return LineID(key)
}
