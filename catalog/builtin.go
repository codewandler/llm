package catalog

import _ "embed"

//go:embed catalog.json
var builtinCatalogJSON []byte

func LoadBuiltIn() (Catalog, error) {
	return LoadJSONBytes(builtinCatalogJSON)
}
