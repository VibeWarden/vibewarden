// Package templates embeds the VibeWarden runtime configuration template
// files into the binary using Go's embed.FS.
package templates

import "embed"

// FS holds all runtime configuration template files embedded at compile time.
// It includes both *.tmpl files and the mappers/ subdirectory containing
// Kratos OIDC Jsonnet mapper files.
//
//go:embed *.tmpl mappers/*.jsonnet
var FS embed.FS
