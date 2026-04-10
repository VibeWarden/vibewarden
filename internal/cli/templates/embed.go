// Package templates embeds the VibeWarden scaffold template files into the
// binary using Go's embed.FS.
package templates

import "embed"

// FS holds all template files embedded at compile time, including shared
// wrap templates (*.tmpl) and agent templates (agents/).
//
//go:embed *.tmpl agents/*.tmpl
var FS embed.FS
