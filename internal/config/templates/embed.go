// Package templates embeds the VibeWarden runtime configuration template
// files into the binary using Go's embed.FS.
package templates

import "embed"

// FS holds all runtime configuration template files embedded at compile time.
//
//go:embed *.tmpl
var FS embed.FS
