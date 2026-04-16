// Package catalog implements a standalone model/service/runtime graph designed
// for eventual extraction into a dedicated module.
//
// Public source adapters stay in the catalog package so consumers of the future
// standalone module can instantiate them directly. Upstream-specific fetch and
// schema helpers live under internal/source/.
package catalog
