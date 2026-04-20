// Package archutil provides shared architecture normalization utilities.
// It maps platform-specific architecture strings (e.g. from "uname -m") to
// Go's runtime.GOARCH naming convention so that local and remote architectures
// can be compared consistently.
package archutil

import "strings"

// Normalize maps the output of "uname -m" (or similar platform identifier) to
// Go's runtime.GOARCH naming convention. This allows consistent comparison
// between local (runtime.GOARCH) and remote (uname -m output) architectures.
//
// Known mappings:
//   - "x86_64"  -> "amd64"
//   - "aarch64" -> "arm64"
//   - "arm64"   -> "arm64"
//   - "armv7l"  -> "arm"
//
// Unknown values are returned as-is (lowercased and trimmed).
func Normalize(unameMachine string) string {
	s := strings.TrimSpace(strings.ToLower(unameMachine))
	switch s {
	case "x86_64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l":
		return "arm"
	default:
		return s
	}
}
