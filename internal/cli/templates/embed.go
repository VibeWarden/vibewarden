// Package templates embeds the VibeWarden scaffold template files into the
// binary using Go's embed.FS.
package templates

import "embed"

// FS holds all template files embedded at compile time, including language pack
// subdirectories (e.g. go/).
//
//go:embed *.tmpl go/*.tmpl
var FS embed.FS
