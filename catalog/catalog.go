package catalog

type Catalog struct {
	Models    map[ModelKey]ModelRecord `json:"-"`
	Services  map[string]Service       `json:"-"`
	Offerings map[OfferingRef]Offering `json:"-"`
}

type RuntimeAccessKey struct {
	RuntimeID   string
	ServiceID   string
	WireModelID string
}

type RuntimeAcquisitionKey struct {
	RuntimeID   string
	ServiceID   string
	WireModelID string
}

type ResolvedCatalog struct {
	Catalog
	Runtimes           map[string]Runtime
	RuntimeAccess      map[RuntimeAccessKey]RuntimeAccess
	RuntimeAcquisition map[RuntimeAcquisitionKey]RuntimeAcquisition
}

func NewCatalog() Catalog {
	return Catalog{
		Models:    make(map[ModelKey]ModelRecord),
		Services:  make(map[string]Service),
		Offerings: make(map[OfferingRef]Offering),
	}
}

func NewResolvedCatalog(base Catalog) ResolvedCatalog {
	out := ResolvedCatalog{
		Catalog: Catalog{
			Models:    make(map[ModelKey]ModelRecord, len(base.Models)),
			Services:  make(map[string]Service, len(base.Services)),
			Offerings: make(map[OfferingRef]Offering, len(base.Offerings)),
		},
		Runtimes:           make(map[string]Runtime),
		RuntimeAccess:      make(map[RuntimeAccessKey]RuntimeAccess),
		RuntimeAcquisition: make(map[RuntimeAcquisitionKey]RuntimeAcquisition),
	}
	for k, v := range base.Models {
		out.Models[k] = v
	}
	for k, v := range base.Services {
		out.Services[k] = v
	}
	for k, v := range base.Offerings {
		out.Offerings[k] = v
	}
	return out
}
