// Package templates embeds the VibeWarden scaffold template files into the
// binary using Go's embed.FS.
package templates

import "embed"

// FS holds all template files embedded at compile time, including shared
// wrap templates (*.tmpl), agent templates (agents/), and prompt-template
// flavors (prompts/).
//
//go:embed *.tmpl agents/*.tmpl prompts/*.tmpl
var FS embed.FS
