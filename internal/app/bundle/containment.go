package bundle

import (
	"os"
	"strings"
)

// containsPath reports whether resolved is absRoot itself or a path strictly
// beneath it.
//
// Both arguments must already be absolute and symlink-resolved (see
// filepath.EvalSymlinks); containsPath performs no normalisation of its own.
//
// The separator-terminated comparison is load-bearing. A bare
// strings.HasPrefix(resolved, absRoot) admits sibling directories whose name
// merely extends the root name, so a symlink pointing at /proj-secret would be
// treated as contained when the project root is /proj. Both bundle walkers
// (the input-digest hash and the staleness scan) share this helper so the two
// containment checks cannot drift apart again (#1274, #1405).
func containsPath(absRoot, resolved string) bool {
	if absRoot == "" {
		return false
	}
	if resolved == absRoot {
		return true
	}
	return strings.HasPrefix(resolved, absRoot+string(os.PathSeparator))
}
