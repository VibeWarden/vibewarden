package bundle

import (
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
)

// DeriveProjectName returns the project name for use in Docker image tags and
// bundle directory naming. It is a thin sanitising wrapper around
// cfg.ComposeProjectName(), which is the canonical resolver (image-name resolution
// unified across vibew bundle and --build; cwd-basename fallback).
//
// cfg.ComposeProjectName() already applies the full derivation chain:
//  1. cfg.Name (always populated by vibew init / vibew wrap since v0.19.0).
//  2. filepath.Base(cfg.ProjectRoot), sanitized — defensive fallback for
//     projects that pre-date the unconditional name: write.
//  3. "vibewarden" as a last-resort.
//
// SanitiseProjectName is applied on top to strip shell-unsafe characters and
// protect README.md / path interpolations from crafted vibewarden.yaml inputs
// (ADR-085 §7, #1061).
func DeriveProjectName(cfg *config.Config, _ string) string {
	return SanitiseProjectName(cfg.ComposeProjectName())
}

// SanitiseProjectName strips any byte outside the shell-safe subset
// [a-zA-Z0-9_-] and returns the result. Returns the empty string when the
// input contains no safe characters — callers fall through to the next
// derivation step in that case.
//
// This is the defensive layer that protects README.md and path interpolation
// from crafted inputs like `myproject" && rm -rf /` in vibewarden.yaml's
// `name:` key. The config schema does not currently validate `name` against
// this subset, so the bundle pipeline carries the guard (ADR-085 §7, #1061).
func SanitiseProjectName(in string) string {
	var b strings.Builder
	b.Grow(len(in))
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
