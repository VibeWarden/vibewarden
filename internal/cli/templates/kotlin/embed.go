// Package kotlintemplates embeds the Kotlin language pack scaffold templates into
// the binary using Go's embed.FS.
package kotlintemplates

import "embed"

// FS holds all Kotlin language pack template files embedded at compile time.
//
//go:embed *.tmpl
var FS embed.FS
