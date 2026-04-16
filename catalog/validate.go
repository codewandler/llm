package catalog

import "fmt"

func ValidateCatalog(c Catalog) error {
	for ref, offering := range c.Offerings {
		if _, ok := c.Services[offering.ServiceID]; !ok {
			return fmt.Errorf("offering %s/%s references unknown service %q", ref.ServiceID, ref.WireModelID, offering.ServiceID)
		}
		if _, ok := c.Models[offering.ModelKey]; !ok {
			return fmt.Errorf("offering %s/%s references unknown model %q", ref.ServiceID, ref.WireModelID, modelID(offering.ModelKey))
		}
	}
	return nil
}

func ValidateResolvedCatalog(c ResolvedCatalog) error {
	if err := ValidateCatalog(c.Catalog); err != nil {
		return err
	}
	for id, runtime := range c.Runtimes {
		if _, ok := c.Services[runtime.ServiceID]; !ok {
			return fmt.Errorf("runtime %q references unknown service %q", id, runtime.ServiceID)
		}
	}
	for key, access := range c.RuntimeAccess {
		if _, ok := c.Runtimes[access.RuntimeID]; !ok {
			return fmt.Errorf("runtime access for %q references unknown runtime %q", key.WireModelID, access.RuntimeID)
		}
		if _, ok := c.Offerings[access.Offering]; !ok {
			return fmt.Errorf("runtime access for %q references unknown offering %q/%q", access.RuntimeID, access.Offering.ServiceID, access.Offering.WireModelID)
		}
	}
	for key, acquisition := range c.RuntimeAcquisition {
		if _, ok := c.Runtimes[acquisition.RuntimeID]; !ok {
			return fmt.Errorf("runtime acquisition for %q references unknown runtime %q", key.WireModelID, acquisition.RuntimeID)
		}
		if _, ok := c.Offerings[acquisition.Offering]; !ok {
			return fmt.Errorf("runtime acquisition for %q references unknown offering %q/%q", acquisition.RuntimeID, acquisition.Offering.ServiceID, acquisition.Offering.WireModelID)
		}
	}
	return nil
}
