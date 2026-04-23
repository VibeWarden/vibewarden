package bundle

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/patternmatcher"
)

// hardIgnoreDirs is the fixed set of directory names that are always excluded
// from the freshness walk, regardless of .gitignore / .dockerignore content.
// These directories must never count toward project freshness because they
// are either VCS artifacts, dependency trees, or build outputs.
var hardIgnoreDirs = []string{
	".git", ".vibewarden", "node_modules", "vendor",
	"dist", "build", "target", ".venv", "__pycache__",
}

// StalenessWalker is a consumer-side test seam: this interface is defined here
// because it is consumed only by this package and its tests; it is not an
// outbound port that crosses a layer boundary.
//
// Do not move to internal/ports/.
//
// StalenessWalker computes the most-recent file mtime under a project root,
// respecting ignore patterns.
type StalenessWalker interface {
	// NewestMTime returns the most recent mtime of any non-ignored file under
	// root, and the count of files whose mtime is strictly after threshold.
	// When root does not exist it returns zero time and zero count with no error.
	NewestMTime(root string, threshold time.Time) (newest time.Time, changedCount int, err error)
}

// FileSystemStalenessWalker is the production implementation of StalenessWalker.
// It reads .gitignore and .dockerignore from root on the first call and compiles
// a combined moby/patternmatcher, then walks the directory tree.
type FileSystemStalenessWalker struct {
	// projectRoot is the directory searched for .gitignore and .dockerignore.
	// It is also the root of the filesystem walk.
	projectRoot string
}

// NewFileSystemStalenessWalker creates a FileSystemStalenessWalker rooted at
// projectRoot. The walk and pattern compilation are deferred to NewestMTime.
func NewFileSystemStalenessWalker(projectRoot string) *FileSystemStalenessWalker {
	return &FileSystemStalenessWalker{projectRoot: projectRoot}
}

// NewestMTime walks the project root and returns:
//   - newest: the most-recent mtime among non-ignored files
//   - changedCount: the number of non-ignored files with mtime strictly after threshold
//   - err: always nil (walker is best-effort; unreadable entries are logged and skipped)
func (w *FileSystemStalenessWalker) NewestMTime(root string, threshold time.Time) (time.Time, int, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return time.Time{}, 0, nil
	}

	pm := buildPatternMatcher(root)
	hardSet := buildHardIgnoreSet()

	var newest time.Time
	changedCount := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Debug("staleness walk: skipping unreadable entry", "path", path, "err", walkErr)
			return nil // best-effort: skip and continue
		}

		// Compute path relative to root for pattern matching.
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		// Convert to forward-slash for cross-platform pattern matching.
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if path == root {
				return nil
			}
			// Skip hard-ignore directories.
			base := d.Name()
			if hardSet[base] {
				return filepath.SkipDir
			}
			// Skip directories matched by .gitignore / .dockerignore.
			if pm != nil {
				matched, err := pm.MatchesOrParentMatches(rel)
				if err == nil && matched {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Regular file: check ignore patterns.
		if pm != nil {
			matched, err := pm.MatchesOrParentMatches(rel)
			if err == nil && matched {
				return nil // ignored
			}
		}

		info, err := d.Info()
		if err != nil {
			slog.Debug("staleness walk: cannot stat file", "path", path, "err", err)
			return nil
		}

		mtime := info.ModTime()
		if mtime.After(newest) {
			newest = mtime
		}
		if mtime.After(threshold) {
			changedCount++
		}

		return nil
	})

	if err != nil {
		slog.Debug("staleness walk: walk error (partial result)", "root", root, "err", err)
	}

	return newest, changedCount, nil
}

// buildPatternMatcher reads .gitignore and .dockerignore from root and returns
// a compiled patternmatcher.PatternMatcher. Returns nil when neither file
// exists or on read error (best-effort).
func buildPatternMatcher(root string) *patternmatcher.PatternMatcher {
	var patterns []string
	for _, name := range []string{".gitignore", ".dockerignore"} {
		p := filepath.Join(root, name)
		lines, err := readIgnoreFile(p)
		if err != nil {
			continue // file absent or unreadable — skip
		}
		patterns = append(patterns, lines...)
	}
	if len(patterns) == 0 {
		return nil
	}
	pm, err := patternmatcher.New(patterns)
	if err != nil {
		slog.Debug("staleness walk: failed to compile ignore patterns", "err", err)
		return nil
	}
	return pm
}

// readIgnoreFile reads a .gitignore or .dockerignore file and returns the
// non-empty, non-comment lines as a slice of pattern strings.
func readIgnoreFile(path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // path is derived from project root
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// buildHardIgnoreSet converts hardIgnoreDirs into a set for O(1) lookup.
func buildHardIgnoreSet() map[string]bool {
	set := make(map[string]bool, len(hardIgnoreDirs))
	for _, d := range hardIgnoreDirs {
		set[d] = true
	}
	return set
}
