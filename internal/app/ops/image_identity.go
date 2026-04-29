package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// LabelProjectRootHash is the authoritative identity label stamped on every
	// vibew-built image. Value format: "sha256:<64-hex>" of the realpath of the
	// project root. Used for equality comparison during dev pre-flight.
	LabelProjectRootHash = "org.vibewarden.project-root-hash"

	// LabelProjectRoot is the informational human-readable project root path
	// stamped alongside the hash. Used only in error messages; never compared.
	LabelProjectRoot = "org.vibewarden.project-root"
)

// ErrProjectRootMismatch is returned by VerifyAppImageIdentity when the
// resolved app image's project-root-hash label is missing or does not match
// the current project root. The error is handled by the CLI layer to render
// the actionable formatted message.
var ErrProjectRootMismatch = errors.New("app image was built from a different project")

// ImageIdentity is the project-identity value stamped on a vibew-built image
// via Docker labels. Both fields are populated from labels read at inspect
// time. A zero ImageIdentity (Hash == "" and Path == "") means the image
// carries no vibew project-root labels — i.e. it was built before this
// feature shipped or by something other than "vibew build".
type ImageIdentity struct {
	// Hash is the value of the org.vibewarden.project-root-hash label,
	// formatted as "sha256:<64-hex>". Empty when the label is absent.
	Hash string
	// Path is the value of the org.vibewarden.project-root label, the
	// human-readable absolute project root used at build time. Empty when
	// the label is absent. Informational only — never used for comparison.
	Path string
}

// IsLabelled reports whether the image carries a vibew project-root hash
// label. Used by the dev pre-flight to distinguish "unlabelled (legacy)"
// from "mismatched (foreign project)".
func (i ImageIdentity) IsLabelled() bool { return i.Hash != "" }

// ProjectRootHash returns the two Docker label values for a project root:
//   - hashLabel: "sha256:<64-hex>" of the realpath of projectRoot.
//   - pathLabel: the realpath verbatim (informational, shown in errors).
//
// It calls filepath.EvalSymlinks to normalise the path before hashing so
// that symlinked clones hash identically to their real directories. On
// EvalSymlinks failure it falls back to filepath.Abs and logs a debug warning.
// On total failure (both EvalSymlinks and Abs fail) it returns an error.
func ProjectRootHash(projectRoot string) (hashLabel, pathLabel string, err error) {
	realPath, evalErr := filepath.EvalSymlinks(projectRoot)
	if evalErr != nil {
		// EvalSymlinks may fail when the path contains symlinks that no longer
		// resolve (e.g. NFS bind-mount that was remounted). Fall back to Abs.
		slog.Debug("EvalSymlinks failed for project root; falling back to Abs",
			"project_root", projectRoot, "error", evalErr)
		absPath, absErr := filepath.Abs(projectRoot)
		if absErr != nil {
			return "", "", fmt.Errorf("resolving project root %q: EvalSymlinks: %w; Abs: %v", projectRoot, evalErr, absErr)
		}
		realPath = absPath
	}

	sum := sha256.Sum256([]byte(realPath))
	return "sha256:" + hex.EncodeToString(sum[:]), realPath, nil
}

// VerifyAppImageIdentity inspects the named image and validates its project
// identity labels against the current project root hash.
//
// It returns:
//   - (identity, nil) when the image's project-root-hash matches expectedHash.
//   - (identity, ErrProjectRootMismatch) when the hash label is missing or
//     differs from expectedHash. The returned identity is populated for use
//     in the formatted error message (both variants).
//   - (zero, err) for any other inspector failure.
//
// The image must already exist in the local daemon — callers must have already
// passed the image-existence pre-flight before calling this function.
func VerifyAppImageIdentity(ctx context.Context, inspector ports.ImageInspector, image, expectedHash string) (ImageIdentity, error) {
	info, err := inspector.Inspect(ctx, image)
	if err != nil {
		return ImageIdentity{}, fmt.Errorf("inspecting app image %q: %w", image, err)
	}

	identity := ImageIdentity{
		Hash: info.Labels[LabelProjectRootHash],
		Path: info.Labels[LabelProjectRoot],
	}

	if identity.Hash == "" || identity.Hash != expectedHash {
		return identity, ErrProjectRootMismatch
	}

	return identity, nil
}
