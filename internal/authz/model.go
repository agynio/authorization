// Package authz holds the canonical OpenFGA authorization model.
//
// model.fga (DSL) is the single source of truth. It is embedded into the
// binary and transformed to JSON at runtime, so there is no second,
// generated copy to keep in sync.
package authz

import _ "embed"

// ModelDSL is the OpenFGA authorization model in DSL form.
//
//go:embed model.fga
var ModelDSL string
