package bundle

import "io/fs"

// DirPerm is the mode used for every directory that `vibew bundle` creates.
//
// Bundle directories hold generated secrets (.env, .credentials), so they are
// owner-only (0700): group and other must not be able to list or traverse
// them. This is the single source of truth — do not reintroduce mode literals
// at MkdirAll call sites, because MkdirAll is a no-op on an existing directory
// and a divergent literal would make the on-disk mode depend on which call
// happens to run first.
const DirPerm fs.FileMode = 0o700
