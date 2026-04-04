// Package agenttemplates embeds the language-agnostic agent scaffold templates
// into the binary using Go's embed.FS.
//
// These templates are shared across all language packs:
//   - agents-vibewarden.md.tmpl — base for AGENTS-VIBEWARDEN.md (vibew-owned)
//   - agents.md.tmpl — minimal user-owned AGENTS.md with reference
//   - claude.md.tmpl — base for CLAUDE.md project instructions
//   - project.md.tmpl — PROJECT.md description template
package agenttemplates

import "embed"

// FS holds all shared agent template files embedded at compile time.
//
//go:embed *.tmpl
var FS embed.FS
