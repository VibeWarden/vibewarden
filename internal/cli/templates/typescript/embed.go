// Package typescripttemplates embeds the TypeScript language pack scaffold
// templates into the binary using Go's embed.FS.
package typescripttemplates

import "embed"

// FS holds all TypeScript language pack template files embedded at compile time.
//
//go:embed *.tmpl
var FS embed.FS
