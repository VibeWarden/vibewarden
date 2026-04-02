// Package agenttemplates embeds the language-agnostic agent scaffold templates
// into the binary using Go's embed.FS.
//
// These templates are shared across all language packs. Adding a new language
// requires only a language-specific dev.md template and project scaffold — the
// architect.md, reviewer.md, and CLAUDE.md templates come from here.
package agenttemplates

import "embed"

// FS holds all shared agent template files embedded at compile time.
//
//go:embed *.tmpl
var FS embed.FS
