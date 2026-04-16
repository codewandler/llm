package catalog

type PreferenceOverlay struct {
	FavoriteModels    []ModelKey
	FavoriteOfferings []OfferingRef
	PreferredServices []string
	PreferredRuntimes []string
	PreferredCreators []string
	PreferredFamilies []string
	Priority          map[OfferingRef]int
}

func preferenceScore(item Item, pref PreferenceOverlay) int {
	score := 0
	for _, key := range pref.FavoriteModels {
		if NormalizeKey(key) == item.Model.Key {
			score += 10000
			break
		}
	}
	for _, ref := range pref.FavoriteOfferings {
		if ref.ServiceID == item.Offering.ServiceID && ref.WireModelID == item.Offering.WireModelID {
			score += 9000
			break
		}
	}
	for i, service := range pref.PreferredServices {
		if normalizeKeyPart(service) == item.Offering.ServiceID {
			score += 5000 - i
			break
		}
	}
	if item.Runtime != nil {
		for i, runtime := range pref.PreferredRuntimes {
			if normalizeKeyPart(runtime) == item.Runtime.ID {
				score += 3000 - i
				break
			}
		}
	}
	for i, creator := range pref.PreferredCreators {
		if normalizeKeyPart(creator) == item.Model.Key.Creator {
			score += 2000 - i
			break
		}
	}
	for i, family := range pref.PreferredFamilies {
		normalized := normalizeKeyPart(family)
		if normalized == item.Model.Key.Family || normalized == item.Model.Key.Series || normalized == item.Model.Key.Variant {
			score += 1000 - i
			break
		}
	}
	if pref.Priority != nil {
		score += pref.Priority[OfferingRef{ServiceID: item.Offering.ServiceID, WireModelID: item.Offering.WireModelID}]
	}
	return score
}
