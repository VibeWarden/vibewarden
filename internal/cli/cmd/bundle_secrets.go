package cmd

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// sensitiveFile records a file inside the bundle output directory that contains
// credentials or key material the user should be aware of before shipping the
// bundle to a remote host.
type sensitiveFile struct {
	// RelPath is the path relative to the bundle output directory.
	RelPath string
	// Description is a short human-readable label for the file's contents.
	Description string
}

// detectSensitiveFiles walks rootDir recursively and returns a sorted slice of
// sensitive files found under it. The detection rules implement ADR-094's
// first-match-wins table:
//
//  1. basename .env → generated environment variables
//  2. basename .credentials → Kratos admin credentials
//  3. relpath kratos/secrets → Kratos cookie and cipher secrets
//  4. any path under kratos/ → Kratos identity store data
//  5. basename *-key.pem → private key material
//  6. basename *.pem → private key material
//  7. basename *.key → private key material
//  8. basename *.token → API token / bearer credential
//
// Directories are skipped. Files that cannot be made relative to rootDir are
// silently skipped (advisory block — no abort). Walk I/O errors are propagated
// as a wrapped error.
func detectSensitiveFiles(rootDir string) ([]sensitiveFile, error) {
	var matches []sensitiveFile

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("scanning bundle for sensitive files: %w", walkErr)
		}
		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			// Advisory block — skip files we cannot relativise rather than abort.
			return nil
		}
		// Normalise separators so rule comparisons are portable.
		rel = filepath.ToSlash(rel)

		if desc, ok := classifySensitiveFile(rel); ok {
			matches = append(matches, sensitiveFile{RelPath: rel, Description: desc})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].RelPath < matches[j].RelPath
	})
	return matches, nil
}

// classifySensitiveFile applies the ADR-094 first-match-wins rule table to a
// single relative path (slash-separated) and returns the description when any
// rule fires. The bool return is false when no rule matches.
func classifySensitiveFile(rel string) (string, bool) {
	base := filepath.Base(rel)

	switch base {
	case ".env":
		return "generated environment variables", true
	case ".credentials":
		return "Kratos admin credentials", true
	}

	// Exact relpath: kratos/secrets (normalised to slash already by caller).
	if rel == "kratos/secrets" {
		return "Kratos cookie and cipher secrets", true
	}

	// Any other file under kratos/.
	if strings.HasPrefix(rel, "kratos/") {
		return "Kratos identity store data", true
	}

	// Key material — order matters: *-key.pem before *.pem.
	if strings.HasSuffix(base, "-key.pem") {
		return "private key material", true
	}
	if strings.HasSuffix(base, ".pem") {
		return "private key material", true
	}
	if strings.HasSuffix(base, ".key") {
		return "private key material", true
	}
	if strings.HasSuffix(base, ".token") {
		return "API token / bearer credential", true
	}

	return "", false
}

// renderSensitiveBlock writes the awareness block to w when matches is
// non-empty. The output format is stable (ADR-094):
//
//	Sensitive files in this bundle:
//	  <relpath>  — <description>
//	  ...
//	These files ship with the bundle when you copy it to a host. If the host or
//	transport is untrusted, generate fresh credentials there instead.
//
// When matches is empty, renderSensitiveBlock writes nothing.
func renderSensitiveBlock(matches []sensitiveFile, w io.Writer) {
	if len(matches) == 0 {
		return
	}
	fmt.Fprintln(w, "Sensitive files in this bundle:")
	for _, m := range matches {
		fmt.Fprintf(w, "  %s  — %s\n", m.RelPath, m.Description)
	}
	fmt.Fprintln(w, "These files ship with the bundle when you copy it to a host. If the host or")
	fmt.Fprintln(w, "transport is untrusted, generate fresh credentials there instead.")
}
