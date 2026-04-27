package bundle

import (
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
)

// DeriveProjectName returns the project name for use in Docker image tags and
// bundle directory naming. It mirrors the derivation chain used by vibew bundle
// and is the single source of truth shared by both the bundle command and the
// validate checks.
//
// Derivation order:
//  1. cfg.Name, sanitised.
//  2. cfg.App.Image, stripped of tag and registry prefix, sanitised.
//  3. ProjectNameFromConfig(absConfigPath) fallback, sanitised.
//
// Every candidate is run through SanitiseProjectName so that downstream path
// and README interpolations cannot be affected by special characters in a
// crafted vibewarden.yaml (ADR-085 §7).
func DeriveProjectName(cfg *config.Config, absConfigPath string) string {
	if name := SanitiseProjectName(cfg.Name); name != "" {
		return name
	}
	if cfg.App.Image != "" {
		image := cfg.App.Image
		if idx := strings.LastIndex(image, ":"); idx > 0 {
			image = image[:idx]
		}
		if idx := strings.LastIndex(image, "/"); idx >= 0 {
			image = image[idx+1:]
		}
		if name := SanitiseProjectName(image); name != "" {
			return name
		}
	}
	return SanitiseProjectName(ProjectNameFromConfig(absConfigPath))
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
