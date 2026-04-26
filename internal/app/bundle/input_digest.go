package bundle

import (
	"bufio"
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
// reading the digest file. A file carrying a different version is treated as
// corrupt and triggers a mtime fallback.
const inputDigestSchemaVersion = 1

// inputDigestGitIgnoreLine is the line appended to the project .gitignore on
// first write of the digest file. File-specific entry (not the whole
// .vibewarden/ dir) so users retain visibility of any future state files.
const inputDigestGitIgnoreLine = ".vibewarden/.input-digest"

// InputDigest is the JSON structure stored at .vibewarden/.input-digest.
//
// It is machine-only state: never commit, never share across machines.
// schema_version gates future format changes. digest is the SHA-256 content
// hash over the sorted input set. inputs is the sorted list of forward-slash
// relative paths that contributed to the digest (diagnostic only — not
// re-verified on read).
type InputDigest struct {
	SchemaVersion int      `json:"schema_version"`
	Digest        string   `json:"digest"`
	Inputs        []string `json:"inputs"`
}

// readInputDigest reads and validates the digest file at
// <projectRoot>/.vibewarden/.input-digest.
//
// Returns (digest, true) when the file exists, parses as valid JSON, has
// schema_version == 1, and carries a "sha256:<64-hex>" digest value.
// Returns ("", false) in all other cases (missing, corrupt, wrong version,
// malformed digest) — callers fall back to the mtime walker. Failures are
// logged at debug level only; they must not surface as errors.
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
		slog.Debug("input-digest: unsupported schema version", "path", path, "version", d.SchemaVersion)
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
// A write failure is logged at warn level but does not fail the caller — the
// next bundle run will fall back to mtime, which is the documented degraded
// behaviour. Callers are responsible for updating .gitignore before calling
// this function so that .gitignore participates in the digest consistently.
func writeInputDigest(projectRoot string, d InputDigest) {
	vibewardenDir := filepath.Join(projectRoot, ".vibewarden")
	if err := os.MkdirAll(vibewardenDir, 0o750); err != nil {
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
// sorted set of non-ignored files.
//
// Algorithm (deterministic, platform-independent):
//
//	h := sha256.New()
//	for each path in sort(walked_files):
//	    h.Write(path_bytes); h.Write(0x00)
//	    h.Write(file_content); h.Write(0x00)
//	return "sha256:" + hex(h.Sum(nil))
//
// Path separators are normalised to forward-slash so the digest is stable
// across operating systems.
func computeInputDigest(projectRoot string) (InputDigest, error) {
	if _, err := os.Stat(projectRoot); os.IsNotExist(err) {
		// Empty / missing root: return a well-formed empty digest.
		return buildDigest(nil, nil), nil
	}

	pm := buildPatternMatcher(projectRoot)
	hardSet := buildHardIgnoreSet()

	type entry struct {
		rel     string
		absPath string
	}

	var entries []entry

	err := filepath.WalkDir(projectRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Debug("input-digest walk: skipping unreadable entry", "path", path, "err", walkErr)
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
			if hardSet[d.Name()] {
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

		entries = append(entries, entry{rel: rel, absPath: path})
		return nil
	})
	if err != nil {
		slog.Debug("input-digest walk: walk error (partial result)", "root", projectRoot, "err", err)
	}

	// Sort by relative path for determinism.
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	var paths []string
	for _, e := range entries {
		paths = append(paths, e.rel)
	}

	// Read contents in sorted order and hash.
	h := sha256.New()
	for _, e := range entries {
		content, err := os.ReadFile(e.absPath) //nolint:gosec // absPath from WalkDir
		if err != nil {
			slog.Debug("input-digest: cannot read file for hashing", "path", e.absPath, "err", err)
			continue
		}
		h.Write([]byte(e.rel))
		h.Write([]byte{0x00})
		h.Write(content)
		h.Write([]byte{0x00})
	}

	return buildDigest(paths, h.Sum(nil)), nil
}

// buildDigest assembles an InputDigest from the sorted input list and raw
// SHA-256 hash bytes.
func buildDigest(inputs []string, hashBytes []byte) InputDigest {
	var digest string
	if hashBytes != nil {
		digest = "sha256:" + hex.EncodeToString(hashBytes)
	} else {
		// Empty walk: stable empty digest.
		empty := sha256.Sum256(nil)
		digest = "sha256:" + hex.EncodeToString(empty[:])
	}
	if inputs == nil {
		inputs = []string{}
	}
	return InputDigest{
		SchemaVersion: inputDigestSchemaVersion,
		Digest:        digest,
		Inputs:        inputs,
	}
}

// isValidDigestString reports whether s matches the pattern "sha256:<64 hex
// digits>".
func isValidDigestString(s string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	hex := s[len(prefix):]
	if len(hex) != 64 {
		return false
	}
	for _, c := range hex {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ensureGitIgnored appends line to <projectRoot>/.gitignore when the line is
// not already present. The operation is idempotent. Lines are compared by
// exact string match after trimming whitespace. If .gitignore does not exist
// it is created.
func ensureGitIgnored(projectRoot, line string) error {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")

	// Read existing lines.
	existing, err := readGitIgnoreLines(gitignorePath)
	if err != nil {
		return fmt.Errorf("reading .gitignore: %w", err)
	}

	// Check if the line is already present.
	for _, l := range existing {
		if strings.TrimSpace(l) == strings.TrimSpace(line) {
			return nil // already present — idempotent
		}
	}

	// Append the line.
	f, err := os.OpenFile(gitignorePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path derived from project root
	if err != nil {
		return fmt.Errorf("opening .gitignore for append: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Add a trailing newline before the new line if the file is non-empty and
	// doesn't end with a newline already.
	if len(existing) > 0 {
		last := existing[len(existing)-1]
		if last != "" {
			// The file had content; ensure we start on a fresh line.
			if _, err := fmt.Fprintln(f, ""); err != nil {
				return fmt.Errorf("writing newline to .gitignore: %w", err)
			}
		}
	}

	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("appending to .gitignore: %w", err)
	}
	return nil
}

// readGitIgnoreLines reads all raw lines from path. Returns nil when the file
// does not exist (not an error). Lines include their content without the
// newline character.
func readGitIgnoreLines(path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // path derived from project root
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}
