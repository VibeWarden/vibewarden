// Package apispec embeds the OpenAPI 3.0 specification for the VibeWarden
// admin API. The canonical human-readable copy lives at docs/openapi.yaml in
// the repository root; this package provides a compiled-in copy for the
// /_vibewarden/api/docs endpoint.
//
// When the spec is updated, docs/openapi.yaml and internal/apispec/openapi.yaml
// must be kept in sync.
package apispec

import _ "embed"

// Spec holds the raw YAML bytes of the OpenAPI 3.0 specification.
// It is embedded at build time from openapi.yaml in this package directory.
//
//go:embed openapi.yaml
var Spec []byte
