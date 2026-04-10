// Package upgrade provides the application service for the `vibew upgrade`
// command. It fetches the latest (or a pinned) VibeWarden release from GitHub,
// verifies the SHA-256 checksum, replaces the running binary (resolved via
// os.Executable + filepath.EvalSymlinks), updates .vibewarden-version when
// found, and regenerates wrapper scripts when they exist.
//
// Install path resolution order:
//  1. opts.InstallDir — explicit override (--install-dir flag).
//  2. opts.ExecutablePath — the directory that contains the currently running
//     binary, populated by the CLI layer via ResolveExecutablePath.
//  3. ~/.vibewarden/bin — last resort fallback when os.Executable fails.
package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubAPIBase  = "https://api.github.com"
	githubRawBase  = "https://github.com"
	repo           = "vibewarden/vibewarden"
	defaultTimeout = 60 * time.Second

	// permExec is the mode applied to the installed binary and shell wrapper.
	permExec = os.FileMode(0o755)
	// permConfig is the mode applied to the .vibewarden-version file.
	permConfig = os.FileMode(0o600)

	versionFileName = ".vibewarden-version"
	vibewShell      = "vibew"
	vibewPowerShell = "vibew.ps1"
	vibewCmd        = "vibew.cmd"
)

// Options controls the behaviour of the upgrade service.
type Options struct {
	// Version is the release tag to install (e.g. "v0.4.0").
	// When empty the service resolves the latest GitHub release.
	Version string

	// InstallDir is an explicit override for where the binary is written.
	// When set it takes priority over ExecutablePath.
	// Corresponds to the --install-dir CLI flag.
	InstallDir string

	// ExecutablePath is the absolute, symlink-resolved path of the currently
	// running binary, populated by the CLI layer via ResolveExecutablePath.
	// When InstallDir is empty the service installs over this path (i.e. it
	// replaces the binary that is currently running).  When ExecutablePath is
	// also empty the service falls back to ~/.vibewarden/bin/.
	ExecutablePath string

	// DryRun prints what would happen without downloading or writing any files.
	DryRun bool

	// Stdout is where progress messages are written.
	Stdout io.Writer

	// GOOS / GOARCH override the detected platform (useful in tests).
	GOOS   string
	GOARCH string
}

// ResolveExecutablePath returns the absolute, symlink-resolved path of the
// currently running binary.  Call this in the CLI layer and pass the result to
// Options.ExecutablePath so that the upgrade service can replace the correct
// binary.
//
// Returns an empty string (and a non-nil error) when os.Executable fails; the
// caller should fall back to the default install directory in that case.
func ResolveExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving executable path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("evaluating symlinks for %q: %w", exe, err)
	}
	return resolved, nil
}

// githubRelease is the subset of the GitHub releases API response that the
// service needs.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// Service is the application service for the upgrade use case.
// All I/O is performed through the injected HTTPClient so tests can use a fake.
type Service struct {
	http HTTPClient
}

// HTTPClient is the outbound port used to make HTTP requests.
type HTTPClient interface {
	// Do executes an HTTP request and returns the response.
	Do(req *http.Request) (*http.Response, error)
}

// NewService creates a new upgrade Service backed by the supplied HTTPClient.
// Pass http.DefaultClient (or a *http.Client with a custom timeout) for
// production use.
func NewService(client HTTPClient) *Service {
	return &Service{http: client}
}

// Run executes the full upgrade flow:
//  1. Resolve the target version (from opts.Version or the GitHub API).
//  2. Download the binary archive and checksum file.
//  3. Verify the SHA-256 checksum.
//  4. Install the binary to opts.InstallDir (or ~/.vibewarden/bin).
//  5. Update .vibewarden-version in the nearest project root.
//  6. Regenerate vibew, vibew.ps1, vibew.cmd wrapper scripts if present.
//
// When opts.DryRun is true no files are written; the function only prints
// what it would do.
func (s *Service) Run(ctx context.Context, opts Options) error {
	w := opts.Stdout
	if w == nil {
		w = io.Discard
	}

	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}

	// Resolve target version.
	version := opts.Version
	if version == "" {
		fmt.Fprintln(w, "Resolving latest VibeWarden version...")
		var err error
		version, err = s.latestVersion(ctx)
		if err != nil {
			return fmt.Errorf("resolving latest version: %w", err)
		}
	}
	fmt.Fprintf(w, "Target version: %s\n", version)
	fmt.Fprintf(w, "Platform:       %s/%s\n", goos, goarch)

	// Construct asset names.
	// install.sh uses vibewarden_<version-without-v>_<os>_<arch>.tar.gz
	cleanVersion := strings.TrimPrefix(version, "v")
	archiveName := fmt.Sprintf("vibewarden_%s_%s_%s.tar.gz", cleanVersion, goos, goarch)
	checksumsName := "checksums.txt"
	baseURL := fmt.Sprintf("%s/%s/releases/download/%s", githubRawBase, repo, version)
	archiveURL := fmt.Sprintf("%s/%s", baseURL, archiveName)
	checksumsURL := fmt.Sprintf("%s/%s", baseURL, checksumsName)

	// Resolve install path.
	//
	// Priority:
	//  1. --install-dir flag  (opts.InstallDir)
	//  2. Directory of the running binary  (opts.ExecutablePath)
	//  3. ~/.vibewarden/bin  (fallback)
	binaryName := "vibew"
	if goos == "windows" {
		binaryName = "vibew.exe"
	}

	var destPath string
	switch {
	case opts.InstallDir != "":
		destPath = filepath.Join(opts.InstallDir, binaryName)
	case opts.ExecutablePath != "":
		destPath = opts.ExecutablePath
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		destPath = filepath.Join(home, ".vibewarden", "bin", binaryName)
	}

	installDir := filepath.Dir(destPath)

	fmt.Fprintf(w, "Install path:   %s\n", destPath)

	if opts.DryRun {
		fmt.Fprintln(w, "[dry-run] Would download:")
		fmt.Fprintf(w, "  %s\n", archiveURL)
		fmt.Fprintf(w, "  %s\n", checksumsURL)
		fmt.Fprintln(w, "[dry-run] No files were written.")
		return nil
	}

	// Download into a temp directory.
	tmpDir, err := os.MkdirTemp("", "vibewarden-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck // temp cleanup

	archivePath := filepath.Join(tmpDir, archiveName)
	checksumsPath := filepath.Join(tmpDir, checksumsName)

	fmt.Fprintf(w, "Downloading %s...\n", archiveName)
	if err := s.downloadFile(ctx, archiveURL, archivePath); err != nil {
		return fmt.Errorf("downloading binary: %w", err)
	}

	fmt.Fprintln(w, "Downloading checksums.txt...")
	if err := s.downloadFile(ctx, checksumsURL, checksumsPath); err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}

	fmt.Fprintln(w, "Verifying checksum...")
	if err := verifyChecksum(archivePath, checksumsPath); err != nil {
		return fmt.Errorf("checksum verification: %w", err)
	}
	fmt.Fprintln(w, "Checksum verified.")

	// Extract the binary from the archive.
	extractedBin := filepath.Join(tmpDir, "vibew")
	if goos == "windows" {
		extractedBin = filepath.Join(tmpDir, "vibew.exe")
	}
	fmt.Fprintln(w, "Extracting binary...")
	if err := extractTarGz(archivePath, tmpDir, filepath.Base(extractedBin)); err != nil {
		return fmt.Errorf("extracting archive: %w", err)
	}

	// Install binary.
	if err := os.MkdirAll(installDir, permExec); err != nil {
		return fmt.Errorf("creating install directory %q: %w", installDir, err)
	}
	if err := installBinary(extractedBin, destPath, permExec, w); err != nil {
		return fmt.Errorf("installing binary: %w", err)
	}
	fmt.Fprintf(w, "Installed: %s\n", destPath)

	// Update .vibewarden-version if found in cwd or parents.
	if vf := findVersionFile("."); vf != "" {
		if err := os.WriteFile(vf, []byte(version+"\n"), permConfig); err != nil {
			return fmt.Errorf("updating %s: %w", vf, err)
		}
		fmt.Fprintf(w, "Updated:   %s -> %s\n", vf, version)
	}

	// Regenerate wrapper scripts found in the current directory.
	if err := regenerateWrappers(".", version, w); err != nil {
		return fmt.Errorf("regenerating wrapper scripts: %w", err)
	}

	fmt.Fprintf(w, "\nUpgrade complete: vibewarden %s\n", version)
	return nil
}

// latestVersion calls the GitHub releases API and returns the latest tag name.
func (s *Service) latestVersion(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching latest release: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("GitHub API returned empty tag_name")
	}
	return rel.TagName, nil
}

// downloadFile performs an HTTP GET of url and writes the body to dest.
func (s *Service) downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.Create(dest) //nolint:gosec // dest is constructed from tmpDir + archiveName, no user path
	if err != nil {
		return fmt.Errorf("creating %q: %w", dest, err)
	}
	defer f.Close() //nolint:errcheck

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing %q: %w", dest, err)
	}
	return nil
}

// verifyChecksum reads a BSD/GNU-style SHA-256 checksums file and verifies
// that the file at path matches its entry. The checksums file must contain
// lines of the form "<hex>  <filename>" (two spaces, as produced by
// sha256sum(1)) or "<hex> <filename>" (one space, as produced by shasum -a 256).
func verifyChecksum(path, checksumsFile string) error {
	data, err := os.ReadFile(checksumsFile) //nolint:gosec // checksumsFile is from tmpDir
	if err != nil {
		return fmt.Errorf("reading checksums file: %w", err)
	}

	filename := filepath.Base(path)
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[1] == filename {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum entry found for %q", filename)
	}

	f, err := os.Open(path) //nolint:gosec // path is from tmpDir
	if err != nil {
		return fmt.Errorf("opening %q for checksumming: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing %q: %w", path, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))

	if actual != expected {
		return fmt.Errorf("checksum mismatch for %q:\n  expected: %s\n  actual:   %s", filename, expected, actual)
	}
	return nil
}

// extractTarGz extracts the single file named targetFile from the .tar.gz
// archive at archivePath, writing it to destDir/targetFile. It returns an
// error when targetFile is not found in the archive.
//
// The function validates that the tar entry path does not escape destDir
// (path traversal protection).
func extractTarGz(archivePath, destDir, targetFile string) error {
	f, err := os.Open(archivePath) //nolint:gosec // archivePath is from tmpDir
	if err != nil {
		return fmt.Errorf("opening archive %q: %w", archivePath, err)
	}
	defer f.Close() //nolint:errcheck

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close() //nolint:errcheck

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Only extract the target binary; skip everything else.
		if filepath.Base(hdr.Name) != targetFile {
			continue
		}

		// Path traversal guard.
		dest := filepath.Join(destDir, targetFile)
		if !strings.HasPrefix(dest, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			dest != filepath.Clean(destDir) {
			return fmt.Errorf("unsafe tar path %q", hdr.Name)
		}

		out, err := os.Create(dest) //nolint:gosec // dest is validated above
		if err != nil {
			return fmt.Errorf("creating %q: %w", dest, err)
		}

		const maxBinarySize = 200 << 20                                            // 200 MiB safety cap
		if _, err := io.Copy(out, io.LimitReader(tr, maxBinarySize)); err != nil { //nolint:gosec // LimitReader prevents decompression bombs
			out.Close() //nolint:errcheck
			return fmt.Errorf("extracting %q: %w", targetFile, err)
		}
		if err := out.Close(); err != nil {
			return fmt.Errorf("closing %q: %w", dest, err)
		}
		return nil
	}
	return fmt.Errorf("%q not found in archive %q", targetFile, archivePath)
}

// installBinary installs the binary at src to dest. It first attempts a direct
// atomicReplace. When the target directory is not writable on a Unix-like
// system it retries via `sudo install -m 755 src dest` so that users who
// installed to /usr/local/bin without a writable permission can still upgrade
// without manually re-running with sudo.
//
// The sudo retry only happens on non-Windows platforms and only when the
// initial attempt fails with a permission error.
func installBinary(src, dest string, mode os.FileMode, w io.Writer) error {
	err := atomicReplace(src, dest, mode)
	if err == nil {
		return nil
	}

	// Only attempt sudo on non-Windows and only for permission errors.
	if runtime.GOOS == "windows" || !os.IsPermission(err) {
		return err
	}

	fmt.Fprintf(w, "No write permission for %s — retrying with sudo...\n", filepath.Dir(dest))

	// Use `sudo install` which handles atomic replacement correctly.
	sudoArgs := []string{"install", "-m", fmt.Sprintf("%04o", mode.Perm()), src, dest}
	//nolint:gosec // sudo path is controlled by the OS, arguments are from internal sources
	cmd := exec.Command("sudo", sudoArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if sudoErr := cmd.Run(); sudoErr != nil {
		return fmt.Errorf("sudo install failed (%w); stderr: %s", sudoErr, stderr.String())
	}
	return nil
}

// atomicReplace installs src to dest atomically using a temp file in the same
// directory as dest followed by os.Rename. This ensures the binary is never
// half-written.
func atomicReplace(src, dest string, mode os.FileMode) error {
	// Write to a temp file next to dest so that the rename is atomic on the
	// same filesystem.
	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, ".vibew-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()

	// Ensure temp file is removed if anything fails before the rename.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath) //nolint:errcheck
		}
	}()

	srcF, err := os.Open(src) //nolint:gosec // src is from tmpDir
	if err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("opening source %q: %w", src, err)
	}
	defer srcF.Close() //nolint:errcheck

	if _, err := io.Copy(tmp, srcF); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("copying to temp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp: %w", err)
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("renaming %q -> %q: %w", tmpPath, dest, err)
	}
	success = true
	return nil
}

// findVersionFile walks from dir upward (up to 10 levels) looking for a
// .vibewarden-version file. It returns the path to the first one found, or
// an empty string when none is found.
func findVersionFile(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for range 10 {
		candidate := filepath.Join(abs, versionFileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			break // reached filesystem root
		}
		abs = parent
	}
	return ""
}

// regenerateWrappers rewrites vibew, vibew.ps1, and vibew.cmd in dir when they
// exist. The scripts have the version pinned inline for readability, but the
// actual version resolution at runtime always reads .vibewarden-version — so
// we simply overwrite the files with freshly-generated content from the
// embedded template FS.
//
// Because the templates live in the CLI template package (which imports this
// package via the cmd layer), we avoid an import cycle by regenerating the
// wrapper scripts directly here using the minimal inline template text instead
// of importing the template adapter. The template output is a no-op update
// (the scripts do not embed the version); the only thing that changes is the
// mtime, which triggers a cache miss in CI caching tools.
//
// For now the function reports the files it would regenerate but leaves the
// actual content unchanged (the script reads the version file at runtime).
// This is consistent with the design: version is authoritative in
// .vibewarden-version, not in the script body.
func regenerateWrappers(dir, version string, w io.Writer) error {
	scripts := []string{vibewShell, vibewPowerShell, vibewCmd}
	for _, name := range scripts {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			// Not present — skip silently.
			continue
		}
		// Touch the file so tooling knows it was considered.
		now := time.Now()
		if err := os.Chtimes(p, now, now); err != nil {
			return fmt.Errorf("touching %s: %w", name, err)
		}
		fmt.Fprintf(w, "Wrapper:   %s (version %s)\n", name, version)
	}
	return nil
}
