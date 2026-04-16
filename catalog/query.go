package catalog

func (c Catalog) ModelByKey(key ModelKey) (ModelRecord, bool) {
	model, ok := c.Models[NormalizeKey(key)]
	return model, ok
}

func (c Catalog) OfferingByRef(ref OfferingRef) (Offering, bool) {
	offering, ok := c.Offerings[OfferingRef{ServiceID: normalizeKeyPart(ref.ServiceID), WireModelID: ref.WireModelID}]
	return offering, ok
}

func (c Catalog) ResolveWireModel(serviceID, wireModelID string) (ModelRecord, bool) {
	offering, ok := c.OfferingByRef(OfferingRef{ServiceID: serviceID, WireModelID: wireModelID})
	if !ok {
		return ModelRecord{}, false
	}
	return c.ModelByKey(offering.ModelKey)
}

func (c Catalog) OfferingsByModel(key ModelKey) []Offering {
	key = NormalizeKey(key)
	out := make([]Offering, 0)
	for _, offering := range c.Offerings {
		if offering.ModelKey == key {
			out = append(out, offering)
		}
	}
	return out
}

func (c Catalog) OfferingsByService(serviceID string) []Offering {
	serviceID = normalizeKeyPart(serviceID)
	out := make([]Offering, 0)
	for _, offering := range c.Offerings {
		if offering.ServiceID == serviceID {
			out = append(out, offering)
		}
	}
	return out
}

func (c ResolvedCatalog) RoutableOfferings(runtimeID string) []Offering {
	runtimeID = normalizeKeyPart(runtimeID)
	out := make([]Offering, 0)
	for _, access := range c.RuntimeAccess {
		if access.RuntimeID != runtimeID || !access.Routable {
			continue
		}
		if offering, ok := c.Offerings[access.Offering]; ok {
			out = append(out, offering)
		}
	}
	return out
}

func (c ResolvedCatalog) VisibleButNotRoutableOfferings(runtimeID string) []Offering {
	runtimeID = normalizeKeyPart(runtimeID)
	out := make([]Offering, 0)
	for _, acquisition := range c.RuntimeAcquisition {
		if acquisition.RuntimeID != runtimeID || !acquisition.Known {
			continue
		}
		if access, ok := c.RuntimeAccess[RuntimeAccessKey{RuntimeID: runtimeID, ServiceID: acquisition.Offering.ServiceID, WireModelID: acquisition.Offering.WireModelID}]; ok && access.Routable {
			continue
		}
		if offering, ok := c.Offerings[acquisition.Offering]; ok {
			out = append(out, offering)
		}
	}
	return out
}

func (c ResolvedCatalog) AcquirableOfferings(runtimeID string) []Offering {
	runtimeID = normalizeKeyPart(runtimeID)
	out := make([]Offering, 0)
	for _, acquisition := range c.RuntimeAcquisition {
		if acquisition.RuntimeID != runtimeID || !acquisition.Acquirable {
			continue
		}
		if offering, ok := c.Offerings[acquisition.Offering]; ok {
			out = append(out, offering)
		}
	}
	return out
}
