// Package templates embeds the VibeWarden runtime configuration template
// files into the binary using Go's embed.FS.
package templates

import "embed"

// FS holds all runtime configuration template files embedded at compile time.
// It includes *.tmpl files, the mappers/ subdirectory containing Kratos OIDC
// Jsonnet mapper files, and the observability/ subdirectory containing
// observability stack config templates and the static Grafana dashboard JSON.
//
//go:embed *.tmpl mappers/*.jsonnet observability/*.tmpl observability/*.json
var FS embed.FS
