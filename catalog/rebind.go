package catalog

import "sort"

type creatorRootIndex struct {
	roots  map[ModelKey]struct{}
	byLine map[ModelKey][]ModelKey
}

func newCreatorRootIndex(roots map[ModelKey]struct{}) creatorRootIndex {
	idx := creatorRootIndex{
		roots:  make(map[ModelKey]struct{}, len(roots)),
		byLine: make(map[ModelKey][]ModelKey),
	}
	for key := range roots {
		normalized := NormalizeKey(key)
		idx.roots[normalized] = struct{}{}
		line := LineKey(normalized)
		idx.byLine[line] = append(idx.byLine[line], normalized)
	}
	for line, keys := range idx.byLine {
		sort.Slice(keys, func(i, j int) bool {
			return creatorRootPreference(keys[i], keys[j])
		})
		idx.byLine[line] = keys
	}
	return idx
}

func (idx creatorRootIndex) canonicalKeyFor(key ModelKey) (ModelKey, bool) {
	normalized := NormalizeKey(key)
	if normalized.ReleaseDate != "" {
		if _, ok := idx.roots[normalized]; ok {
			return normalized, true
		}
	}

	line := LineKey(normalized)
	candidates := idx.byLine[line]
	if len(candidates) == 0 {
		return ModelKey{}, false
	}
	return candidates[0], true
}

func rebindFragmentToCreatorRoots(frag *Fragment, idx creatorRootIndex) {
	if frag == nil {
		return
	}
	for i := range frag.Models {
		if key, ok := idx.canonicalKeyFor(frag.Models[i].Key); ok {
			frag.Models[i].Key = key
		}
	}
	for i := range frag.Offerings {
		if key, ok := idx.canonicalKeyFor(frag.Offerings[i].ModelKey); ok {
			frag.Offerings[i].ModelKey = key
		}
	}
}

func creatorRootPreference(left, right ModelKey) bool {
	left = NormalizeKey(left)
	right = NormalizeKey(right)

	leftReleased := left.ReleaseDate != ""
	rightReleased := right.ReleaseDate != ""
	if leftReleased != rightReleased {
		return leftReleased
	}
	if left.ReleaseDate != right.ReleaseDate {
		return left.ReleaseDate > right.ReleaseDate
	}
	return modelID(left) < modelID(right)
}
