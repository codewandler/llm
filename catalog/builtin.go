package catalog

import _ "embed"

// The built-in snapshot is regenerated via:
//
//	go generate ./...
//
//go:embed catalog.json
var builtinCatalogJSON []byte

func LoadBuiltIn() (Catalog, error) {
	return LoadJSONBytes(builtinCatalogJSON)
}
