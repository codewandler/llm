package catalog

import (
	"sort"
	"strings"
)

type ItemFilter func(Item) bool

type ViewOptions struct {
	VisibleOnly       bool
	RoutableOnly      bool
	AliasOverlay      *AliasOverlay
	PreferenceOverlay *PreferenceOverlay
	Filters           []ItemFilter
}

type Item struct {
	Model       ModelRecord
	Offering    Offering
	Runtime     *Runtime
	Access      *RuntimeAccess
	Acquisition *RuntimeAcquisition
}

type View struct {
	items   []Item
	aliases map[string][]int
}

func ServiceView(c Catalog, serviceID string, opts ViewOptions) View {
	serviceID = normalizeKeyPart(serviceID)
	items := make([]Item, 0)
	for _, offering := range c.OfferingsByService(serviceID) {
		model, ok := c.Models[offering.ModelKey]
		if !ok {
			continue
		}
		items = append(items, Item{Model: model, Offering: offering})
	}
	return buildView(items, opts)
}

func RuntimeView(c ResolvedCatalog, runtimeID string, opts ViewOptions) View {
	runtimeID = normalizeKeyPart(runtimeID)
	itemsByRef := make(map[OfferingRef]Item)

	for ref, offering := range c.Offerings {
		model, ok := c.Models[offering.ModelKey]
		if !ok {
			continue
		}
		itemsByRef[ref] = Item{Model: model, Offering: offering}
	}

	for _, runtime := range c.Runtimes {
		if runtime.ID != runtimeID {
			continue
		}
		for ref, item := range itemsByRef {
			if ref.ServiceID == runtime.ServiceID {
				r := runtime
				item.Runtime = &r
				itemsByRef[ref] = item
			}
		}
	}

	for _, acquisition := range c.RuntimeAcquisition {
		if acquisition.RuntimeID != runtimeID {
			continue
		}
		item, ok := itemsByRef[acquisition.Offering]
		if !ok {
			continue
		}
		a := acquisition
		item.Acquisition = &a
		itemsByRef[acquisition.Offering] = item
	}

	for _, access := range c.RuntimeAccess {
		if access.RuntimeID != runtimeID {
			continue
		}
		item, ok := itemsByRef[access.Offering]
		if !ok {
			continue
		}
		a := access
		item.Access = &a
		itemsByRef[access.Offering] = item
	}

	items := make([]Item, 0, len(itemsByRef))
	for _, item := range itemsByRef {
		if item.Runtime == nil {
			continue
		}
		if opts.RoutableOnly {
			if item.Access == nil || !item.Access.Routable {
				continue
			}
		} else if opts.VisibleOnly && (item.Acquisition == nil || !item.Acquisition.Known) {
			continue
		}
		items = append(items, item)
	}

	return buildView(items, opts)
}

func (v View) Resolve(name string) (Item, bool) {
	idxs, ok := v.aliases[name]
	if !ok || len(idxs) == 0 {
		return Item{}, false
	}
	return v.items[idxs[0]], true
}

func (v View) ResolveAll(name string) []Item {
	idxs, ok := v.aliases[name]
	if !ok || len(idxs) == 0 {
		return nil
	}
	out := make([]Item, 0, len(idxs))
	for _, idx := range idxs {
		out = append(out, v.items[idx])
	}
	return out
}

func (v View) AliasNames() []string {
	names := make([]string, 0, len(v.aliases))
	for name := range v.aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (v View) Find(fn ItemFilter) (Item, bool) {
	for _, item := range v.items {
		if fn(item) {
			return item, true
		}
	}
	return Item{}, false
}

func (v View) FindAll(fn ItemFilter) []Item {
	out := make([]Item, 0)
	for _, item := range v.items {
		if fn(item) {
			out = append(out, item)
		}
	}
	return out
}

func (v View) List() []Item {
	out := make([]Item, len(v.items))
	copy(out, v.items)
	return out
}

func (v View) Filter(filters ...ItemFilter) View {
	if len(filters) == 0 {
		return v
	}
	items := make([]Item, 0, len(v.items))
	for _, item := range v.items {
		keep := true
		for _, filter := range filters {
			if filter != nil && !filter(item) {
				keep = false
				break
			}
		}
		if keep {
			items = append(items, item)
		}
	}
	return buildView(items, ViewOptions{})
}

func buildView(items []Item, opts ViewOptions) View {
	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		keep := true
		for _, filter := range opts.Filters {
			if filter != nil && !filter(item) {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, item)
		}
	}
	if opts.PreferenceOverlay != nil {
		sort.SliceStable(filtered, func(i, j int) bool {
			return preferenceScore(filtered[i], *opts.PreferenceOverlay) > preferenceScore(filtered[j], *opts.PreferenceOverlay)
		})
	}
	v := View{items: filtered, aliases: make(map[string][]int)}
	for i, item := range v.items {
		for _, alias := range itemAliases(item) {
			v.aliases[alias] = append(v.aliases[alias], i)
		}
	}
	if opts.AliasOverlay != nil {
		v = v.WithAliasOverlay(*opts.AliasOverlay)
	}
	return v
}

func itemAliases(item Item) []string {
	aliases := make(map[string]struct{})
	ordered := make([]string, 0)
	add := func(alias string) {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			return
		}
		if _, ok := aliases[alias]; ok {
			return
		}
		aliases[alias] = struct{}{}
		ordered = append(ordered, alias)
	}
	add(item.Offering.WireModelID)
	for _, alias := range item.Offering.Aliases {
		add(alias)
	}
	for _, alias := range item.Model.Aliases {
		add(alias)
	}
	if item.Access != nil && item.Access.ResolvedWireID != "" {
		add(item.Access.ResolvedWireID)
	}
	return ordered
}
