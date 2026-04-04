// Package templates embeds the VibeWarden scaffold template files into the
// binary using Go's embed.FS.
package templates

import "embed"

// FS holds all template files embedded at compile time, including language pack
// subdirectories (e.g. go/) and shared agent templates (agents/).
// The commands/ subdirectory holds static Markdown skill files for Claude Code
// slash commands; they are plain .md files, not Go templates.
//
//go:embed *.tmpl go/*.tmpl agents/*.tmpl kotlin/*.tmpl typescript/*.tmpl commands/shared/*.md commands/go/*.md commands/kotlin/*.md commands/typescript/*.md
var FS embed.FS
