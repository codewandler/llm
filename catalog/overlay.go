package catalog

import "strings"

type AliasBinding struct {
	Name    string
	Target  OfferingRef
	Runtime string
}

type AliasOverlay struct {
	Bindings []AliasBinding
}

func (v View) WithAliasOverlay(overlay AliasOverlay) View {
	if len(overlay.Bindings) == 0 {
		return v
	}
	out := View{items: v.List(), aliases: make(map[string][]int, len(v.aliases))}
	for alias, idxs := range v.aliases {
		copied := make([]int, len(idxs))
		copy(copied, idxs)
		out.aliases[alias] = copied
	}
	indexByRef := make(map[OfferingRef]int, len(out.items))
	for i, item := range out.items {
		indexByRef[OfferingRef{ServiceID: item.Offering.ServiceID, WireModelID: item.Offering.WireModelID}] = i
	}
	for _, binding := range overlay.Bindings {
		idx, ok := indexByRef[binding.Target]
		if !ok {
			continue
		}
		name := strings.TrimSpace(binding.Name)
		if name == "" {
			continue
		}
		out.aliases[name] = appendAliasIndex(out.aliases[name], idx)
	}
	return out
}

func appendAliasIndex(indexes []int, idx int) []int {
	for _, existing := range indexes {
		if existing == idx {
			return indexes
		}
	}
	return append(indexes, idx)
}
