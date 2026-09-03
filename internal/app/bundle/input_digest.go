package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// inputDigestFileName is the file name (relative to .vibewarden/) where the
// content digest is stored after a successful vibew bundle run.
const inputDigestFileName = ".vibewarden/.input-digest"

// inputDigestSchemaVersion is the schema version written to and required when
// reading the digest file. Files with a different version are treated as
// missing (first-run baseline), never as an mtime fallback.
//
// History:
//
//	v1 — single "digest" field, no per-file hashes. Dropped in #1223.
//	v2 — per-file SHA-256 in "files" array; supports changed-path rendering.
const inputDigestSchemaVersion = 2

// InputDigest is the JSON structure stored at .vibewarden/.input-digest.
//
// It is machine-only state: never commit, never share across machines.
// schema_version gates future format changes. digest is the SHA-256 content
// hash over the sorted input set. files is the sorted list of {path, sha256}
// entries that contributed to the digest — used to render the changed-path
// list in the freshness warning.
type InputDigest struct {
	SchemaVersion int             `json:"schema_version"`
	Digest        string          `json:"digest"`
	Files         []InputFileHash `json:"files"`
}

// InputFileHash is one entry in InputDigest.Files. Path is a forward-slash
// relative path from the project root. Hash is "sha256:<64 hex digits>".
type InputFileHash struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

// ChangedPathKind classifies the type of change detected between two digests.
type ChangedPathKind string

const (
	// ChangedPathAdded means the file was not present in the prior digest.
	ChangedPathAdded ChangedPathKind = "added"
	// ChangedPathRemoved means the file was present in the prior digest but is
	// gone in the current digest.
	ChangedPathRemoved ChangedPathKind = "removed"
	// ChangedPathModified means the file exists in both digests with different
	// content hashes.
	ChangedPathModified ChangedPathKind = "modified"
)

// ChangedPath records a single file change between two digest snapshots.
type ChangedPath struct {
	// Path is the project-root-relative forward-slash path of the changed file.
	Path string
	// Kind classifies the change.
	Kind ChangedPathKind
}

// maxChangedPathsRendered is the maximum number of changed paths printed in
// the freshness block. Paths beyond this limit are summarised as "(and N more)".
const maxChangedPathsRendered = 5

// readInputDigest reads and validates the digest file at
// <projectRoot>/.vibewarden/.input-digest.
//
// Returns (digest, true) when the file exists, parses as valid JSON, has
// schema_version == 2, and carries a "sha256:<64-hex>" digest value.
// Returns (InputDigest{}, false) in all other cases (missing, corrupt, wrong
// version, malformed digest) — callers treat this as first-run baseline
// (FRESH, no prior comparison). Failures are logged at debug level only.
func readInputDigest(projectRoot string) (InputDigest, bool) {
	path := filepath.Join(projectRoot, inputDigestFileName)
	data, err := os.ReadFile(path) //nolint:gosec // path derived from project root
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("input-digest: cannot read digest file", "path", path, "err", err)
		}
		return InputDigest{}, false
	}

	var d InputDigest
	if err := json.Unmarshal(data, &d); err != nil {
		slog.Debug("input-digest: cannot parse digest file", "path", path, "err", err)
		return InputDigest{}, false
	}

	if d.SchemaVersion != inputDigestSchemaVersion {
		slog.Debug("input-digest: unsupported schema version — treating as missing (first-run baseline)",
			"path", path, "version", d.SchemaVersion, "supported", inputDigestSchemaVersion)
		return InputDigest{}, false
	}

	if !isValidDigestString(d.Digest) {
		slog.Debug("input-digest: invalid digest value", "path", path, "digest", d.Digest)
		return InputDigest{}, false
	}

	return d, true
}

// writeInputDigest writes the digest to
// <projectRoot>/.vibewarden/.input-digest.
//
// A write failure is logged at warn level but does not fail the caller. The
// next bundle run will treat the missing file as a first-run baseline (FRESH),
// which is the correct degraded behaviour.
func writeInputDigest(projectRoot string, d InputDigest) {
	vibewardenDir := filepath.Join(projectRoot, ".vibewarden")
	if err := os.MkdirAll(vibewardenDir, DirPerm); err != nil {
		slog.Warn("input-digest: cannot create .vibewarden dir", "err", err)
		return
	}

	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		slog.Warn("input-digest: cannot marshal digest", "err", err)
		return
	}
	data = append(data, '\n')

	path := filepath.Join(projectRoot, inputDigestFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil { //nolint:gosec // path derived from project root
		slog.Warn("input-digest: cannot write digest file", "path", path, "err", err)
	}
}

// computeInputDigest walks projectRoot using the same ignore rules as the
// FileSystemStalenessWalker and computes a SHA-256 content digest over the
// sorted set of non-ignored files. The returned InputDigest is schema v2: it
// carries a per-file SHA-256 hash in the Files array (used for changed-path
// rendering) and a rolled-up digest for equality comparison.
//
// Algorithm (v2, deterministic, platform-independent):
//
// Step 1 — per-file hashes:
//
//	for each file in sort(walked_files):
//	    fileHash = sha256(file_content)
//	    append {path, "sha256:" + hex(fileHash)} to Files
//
// Step 2 — rolled-up digest:
//
//	h := sha256.New()
//	for each {path, hash} in Files:
//	    h.Write(path_bytes); h.Write(0x00)
//	    h.Write(hash_bytes); h.Write(0x00)
//	Digest = "sha256:" + hex(h.Sum(nil))
//
// Path separators are normalised to forward-slash so the digest is stable
// across operating systems.
//
// Symlinks are resolved via filepath.EvalSymlinks before content is read. If
// the resolved target escapes projectRoot the entry is skipped (mirrors Docker
// build context behaviour and prevents traversal of symlink trees outside the
// project).
func computeInputDigest(projectRoot string) (InputDigest, error) {
	if _, err := os.Stat(projectRoot); os.IsNotExist(err) {
		// Empty / missing root: return a well-formed empty digest.
		return buildDigest(nil), nil
	}

	absRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		absRoot = projectRoot // best-effort: use as-is
	}

	pm := buildPatternMatcher(projectRoot)
	hardSet := buildHardIgnoreSet()
	hardRelSet := buildHardIgnoreRelSet()

	type entry struct {
		rel     string
		absPath string
	}

	var entries []entry

	walkErr := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, wErr error) error {
		if wErr != nil {
			slog.Debug("input-digest walk: skipping unreadable entry", "path", path, "err", wErr)
			return nil
		}

		rel, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if path == projectRoot {
				return nil
			}
			if isHardIgnoredDir(hardSet, hardRelSet, d.Name(), rel) {
				return filepath.SkipDir
			}
			if pm != nil {
				matched, err := pm.MatchesOrParentMatches(rel)
				if err == nil && matched {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Regular file: apply ignore patterns.
		if pm != nil {
			matched, err := pm.MatchesOrParentMatches(rel)
			if err == nil && matched {
				return nil
			}
		}

		// Resolve symlinks to catch loops and out-of-root targets.
		absPath := path
		if d.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				slog.Debug("input-digest: cannot resolve symlink, skipping", "path", path, "err", err)
				return nil
			}
			// Skip if resolved target escapes project root. containsPath
			// (containment.go) is shared with the staleness walker so the two
			// checks cannot drift apart.
			if !containsPath(absRoot, resolved) {
				slog.Debug("input-digest: symlink escapes project root, skipping",
					"path", path, "resolved", resolved)
				return nil
			}
			absPath = resolved
		}

		entries = append(entries, entry{rel: rel, absPath: absPath})
		return nil
	})
	if walkErr != nil {
		slog.Debug("input-digest walk: walk error (partial result)", "root", projectRoot, "err", walkErr)
	}

	// Sort by relative path for determinism.
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	// Read contents in sorted order and hash.
	h := sha256.New()
	var fileHashes []InputFileHash
	for _, e := range entries {
		content, err := os.ReadFile(e.absPath) //nolint:gosec // absPath from WalkDir or EvalSymlinks
		if err != nil {
			slog.Debug("input-digest: cannot read file for hashing", "path", e.absPath, "err", err)
			continue
		}
		h.Write([]byte(e.rel))
		h.Write([]byte{0x00})
		h.Write(content)
		h.Write([]byte{0x00})

		fileHash := sha256.Sum256(content)
		fileHashes = append(fileHashes, InputFileHash{
			Path: e.rel,
			Hash: "sha256:" + hex.EncodeToString(fileHash[:]),
		})
	}

	return buildDigest(fileHashes), nil
}

// buildDigest assembles an InputDigest from the per-file hash list.
func buildDigest(files []InputFileHash) InputDigest {
	// Compute rolled-up digest from all file hashes for the equality fast path.
	h := sha256.New()
	for _, f := range files {
		h.Write([]byte(f.Path))
		h.Write([]byte{0x00})
		h.Write([]byte(f.Hash))
		h.Write([]byte{0x00})
	}

	if files == nil {
		files = []InputFileHash{}
	}
	return InputDigest{
		SchemaVersion: inputDigestSchemaVersion,
		Digest:        "sha256:" + hex.EncodeToString(h.Sum(nil)),
		Files:         files,
	}
}

// diffDigests computes the ordered list of changed paths between prior and
// current digests. The result is sorted: removed first, then added, then
// modified; alphabetically within each bucket. At most maxChangedPathsRendered
// entries are returned; the TotalChanged field on the caller side shows the
// total count.
func diffDigests(prior, current InputDigest) []ChangedPath {
	priorMap := make(map[string]string, len(prior.Files))
	for _, f := range prior.Files {
		priorMap[f.Path] = f.Hash
	}
	currentMap := make(map[string]string, len(current.Files))
	for _, f := range current.Files {
		currentMap[f.Path] = f.Hash
	}

	var removed, added, modified []string

	for path, hash := range priorMap {
		curHash, exists := currentMap[path]
		if !exists {
			removed = append(removed, path)
		} else if curHash != hash {
			modified = append(modified, path)
		}
	}
	for path := range currentMap {
		if _, exists := priorMap[path]; !exists {
			added = append(added, path)
		}
	}

	sort.Strings(removed)
	sort.Strings(added)
	sort.Strings(modified)

	var all []ChangedPath
	for _, p := range removed {
		all = append(all, ChangedPath{Path: p, Kind: ChangedPathRemoved})
	}
	for _, p := range added {
		all = append(all, ChangedPath{Path: p, Kind: ChangedPathAdded})
	}
	for _, p := range modified {
		all = append(all, ChangedPath{Path: p, Kind: ChangedPathModified})
	}

	return all
}

// isValidDigestString reports whether s matches the pattern "sha256:<64 hex
// digits>".
func isValidDigestString(s string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	hexPart := s[len(prefix):]
	if len(hexPart) != 64 {
		return false
	}
	for _, c := range hexPart {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ensureVibewardenGitignoreFile writes <projectRoot>/.vibewarden/.gitignore
// with the content "*\n" so that git treats the entire .vibewarden/ directory
// as ignored without touching the user's own .gitignore. The write is
// idempotent: if the file already exists with the exact correct content, it is
// not rewritten (avoids bumping mtime on every bundle).
func ensureVibewardenGitignoreFile(projectRoot string) error {
	dir := filepath.Join(projectRoot, ".vibewarden")
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("creating .vibewarden dir: %w", err)
	}

	path := filepath.Join(dir, ".gitignore")
	const want = "*\n"

	existing, err := os.ReadFile(path) //nolint:gosec // path derived from project root
	if err == nil && string(existing) == want {
		return nil // already correct — idempotent, do not touch
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading .vibewarden/.gitignore: %w", err)
	}

	if err := os.WriteFile(path, []byte(want), 0o600); err != nil { //nolint:gosec // path derived from project root
		return fmt.Errorf("writing .vibewarden/.gitignore: %w", err)
	}
	return nil
}
