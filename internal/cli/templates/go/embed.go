// Package gotemplates embeds the Go language pack scaffold templates into the
// binary using Go's embed.FS.
package gotemplates

import "embed"

// FS holds all Go language pack template files embedded at compile time.
//
//go:embed *.tmpl
var FS embed.FS
