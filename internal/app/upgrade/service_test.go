package upgrade_test

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
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/upgrade"
)

// fakeHTTPClient implements upgrade.HTTPClient and serves pre-programmed
// responses without touching the network.
type fakeHTTPClient struct {
	// responses maps URL to the body bytes and status code to return.
	responses map[string]fakeResponse
}

type fakeResponse struct {
	status int
	body   []byte
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	resp, ok := f.responses[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
		}, nil
	}
	return &http.Response{
		StatusCode: resp.status,
		Body:       io.NopCloser(bytes.NewReader(resp.body)),
	}, nil
}

// errHTTPClient always returns a transport-level error.
type errHTTPClient struct{ err error }

func (e *errHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	return nil, e.err
}

// buildFakeArchive creates an in-memory tar.gz with a single file named
// binaryName containing content. It also returns the SHA-256 hex digest of the
// archive bytes.
func buildFakeArchive(t *testing.T, binaryName, content string) (archiveBytes []byte, checksumHex string) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("writing tar header: %v", err)
	}
	if _, err := io.WriteString(tw, content); err != nil {
		t.Fatalf("writing tar content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}

	data := buf.Bytes()
	h := sha256.Sum256(data)
	return data, hex.EncodeToString(h[:])
}

// buildChecksums returns a checksums file body containing one entry for the
// given filename and hex digest.
func buildChecksums(filename, hexDigest string) []byte {
	return []byte(fmt.Sprintf("%s  %s\n", hexDigest, filename))
}

// releaseAPIBody returns the JSON body for a fake GitHub latest-release response.
func releaseAPIBody(tag string) []byte {
	b, _ := json.Marshal(map[string]string{"tag_name": tag})
	return b
}

func TestService_Run_DryRun(t *testing.T) {
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			"https://api.github.com/repos/vibewarden/vibewarden/releases/latest": {
				status: http.StatusOK,
				body:   releaseAPIBody("v0.9.0"),
			},
		},
	}
	svc := upgrade.NewService(client)

	var out strings.Builder
	opts := upgrade.Options{
		DryRun: true,
		Stdout: &out,
		GOOS:   "linux",
		GOARCH: "amd64",
	}

	if err := svc.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "[dry-run]") {
		t.Errorf("expected [dry-run] in output, got:\n%s", got)
	}
	if !strings.Contains(got, "v0.9.0") {
		t.Errorf("expected version v0.9.0 in output, got:\n%s", got)
	}
}

func TestService_Run_DryRunWithExplicitVersion(t *testing.T) {
	// No API call should be made when version is explicitly specified.
	client := &fakeHTTPClient{responses: map[string]fakeResponse{}}
	svc := upgrade.NewService(client)

	var out strings.Builder
	opts := upgrade.Options{
		Version: "v1.2.3",
		DryRun:  true,
		Stdout:  &out,
		GOOS:    "darwin",
		GOARCH:  "arm64",
	}

	if err := svc.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "v1.2.3") {
		t.Errorf("expected v1.2.3 in output, got:\n%s", got)
	}
}

func TestService_Run_LatestVersionAPIError(t *testing.T) {
	client := &errHTTPClient{err: fmt.Errorf("network error")}
	svc := upgrade.NewService(client)

	opts := upgrade.Options{
		Stdout: io.Discard,
		GOOS:   "linux",
		GOARCH: "amd64",
	}

	err := svc.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "resolving latest version") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestService_Run_LatestVersionAPIBadStatus(t *testing.T) {
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			"https://api.github.com/repos/vibewarden/vibewarden/releases/latest": {
				status: http.StatusForbidden,
				body:   []byte(`{"message":"rate limited"}`),
			},
		},
	}
	svc := upgrade.NewService(client)

	opts := upgrade.Options{
		Stdout: io.Discard,
		GOOS:   "linux",
		GOARCH: "amd64",
	}

	err := svc.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestService_Run_SuccessfulInstall(t *testing.T) {
	version := "v0.5.0"
	goos := "linux"
	goarch := "amd64"
	cleanVersion := "0.5.0"
	archiveName := fmt.Sprintf("vibewarden_%s_%s_%s.tar.gz", cleanVersion, goos, goarch)

	archiveBytes, checksumHex := buildFakeArchive(t, "vibew", "#!/bin/sh\necho vibewarden")
	checksumBytes := buildChecksums(archiveName, checksumHex)

	baseURL := fmt.Sprintf("https://github.com/vibewarden/vibewarden/releases/download/%s", version)
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			baseURL + "/" + archiveName: {status: http.StatusOK, body: archiveBytes},
			baseURL + "/checksums.txt":  {status: http.StatusOK, body: checksumBytes},
		},
	}
	svc := upgrade.NewService(client)

	installDir := t.TempDir()
	var out strings.Builder
	opts := upgrade.Options{
		Version:    version,
		InstallDir: installDir,
		Stdout:     &out,
		GOOS:       goos,
		GOARCH:     goarch,
	}

	if err := svc.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() unexpected error: %v\nOutput:\n%s", err, out.String())
	}

	// Binary must exist and be executable.
	binPath := filepath.Join(installDir, "vibew")
	info, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("binary not found at %s: %v", binPath, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("binary not executable, mode=%v", info.Mode())
	}

	// Output must mention the version.
	if !strings.Contains(out.String(), version) {
		t.Errorf("output missing version %s:\n%s", version, out.String())
	}
}

func TestService_Run_ChecksumMismatch(t *testing.T) {
	version := "v0.5.0"
	goos := "linux"
	goarch := "amd64"
	cleanVersion := "0.5.0"
	archiveName := fmt.Sprintf("vibewarden_%s_%s_%s.tar.gz", cleanVersion, goos, goarch)

	archiveBytes, _ := buildFakeArchive(t, "vibew", "fake binary content")
	// Use a deliberately wrong checksum.
	badChecksum := strings.Repeat("0", 64)
	checksumBytes := buildChecksums(archiveName, badChecksum)

	baseURL := fmt.Sprintf("https://github.com/vibewarden/vibewarden/releases/download/%s", version)
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			baseURL + "/" + archiveName: {status: http.StatusOK, body: archiveBytes},
			baseURL + "/checksums.txt":  {status: http.StatusOK, body: checksumBytes},
		},
	}
	svc := upgrade.NewService(client)

	installDir := t.TempDir()
	opts := upgrade.Options{
		Version:    version,
		InstallDir: installDir,
		Stdout:     io.Discard,
		GOOS:       goos,
		GOARCH:     goarch,
	}

	err := svc.Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected checksum error, got: %v", err)
	}
}

func TestService_Run_UpdatesVersionFile(t *testing.T) {
	version := "v0.6.0"
	goos := "linux"
	goarch := "amd64"
	cleanVersion := "0.6.0"
	archiveName := fmt.Sprintf("vibewarden_%s_%s_%s.tar.gz", cleanVersion, goos, goarch)

	archiveBytes, checksumHex := buildFakeArchive(t, "vibew", "#!/bin/sh")
	checksumBytes := buildChecksums(archiveName, checksumHex)

	baseURL := fmt.Sprintf("https://github.com/vibewarden/vibewarden/releases/download/%s", version)
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			baseURL + "/" + archiveName: {status: http.StatusOK, body: archiveBytes},
			baseURL + "/checksums.txt":  {status: http.StatusOK, body: checksumBytes},
		},
	}
	svc := upgrade.NewService(client)

	// Create a temp project dir with .vibewarden-version and change into it so
	// findVersionFile("." ) finds it.
	projectDir := t.TempDir()
	vfPath := filepath.Join(projectDir, ".vibewarden-version")
	if err := os.WriteFile(vfPath, []byte("v0.5.0\n"), 0o600); err != nil {
		t.Fatalf("writing version file: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) }) //nolint:errcheck

	installDir := t.TempDir()
	opts := upgrade.Options{
		Version:    version,
		InstallDir: installDir,
		Stdout:     io.Discard,
		GOOS:       goos,
		GOARCH:     goarch,
	}

	if err := svc.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	got, err := os.ReadFile(vfPath)
	if err != nil {
		t.Fatalf("reading version file: %v", err)
	}
	if !strings.Contains(string(got), version) {
		t.Errorf(".vibewarden-version: want %s, got %s", version, string(got))
	}
}

func TestService_Run_RegeneratesWrappers(t *testing.T) {
	version := "v0.7.0"
	goos := "linux"
	goarch := "amd64"
	cleanVersion := "0.7.0"
	archiveName := fmt.Sprintf("vibewarden_%s_%s_%s.tar.gz", cleanVersion, goos, goarch)

	archiveBytes, checksumHex := buildFakeArchive(t, "vibew", "#!/bin/sh")
	checksumBytes := buildChecksums(archiveName, checksumHex)

	baseURL := fmt.Sprintf("https://github.com/vibewarden/vibewarden/releases/download/%s", version)
	client := &fakeHTTPClient{
		responses: map[string]fakeResponse{
			baseURL + "/" + archiveName: {status: http.StatusOK, body: archiveBytes},
			baseURL + "/checksums.txt":  {status: http.StatusOK, body: checksumBytes},
		},
	}
	svc := upgrade.NewService(client)

	projectDir := t.TempDir()
	// Create wrapper scripts so regenerateWrappers finds them.
	for _, name := range []string{"vibew", "vibew.ps1", "vibew.cmd"} {
		p := filepath.Join(projectDir, name)
		if err := os.WriteFile(p, []byte("old content"), 0o755); err != nil {
			t.Fatalf("creating %s: %v", name, err)
		}
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) }) //nolint:errcheck

	installDir := t.TempDir()
	var out strings.Builder
	opts := upgrade.Options{
		Version:    version,
		InstallDir: installDir,
		Stdout:     &out,
		GOOS:       goos,
		GOARCH:     goarch,
	}

	if err := svc.Run(context.Background(), opts); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	output := out.String()
	for _, name := range []string{"vibew", "vibew.ps1", "vibew.cmd"} {
		if !strings.Contains(output, name) {
			t.Errorf("expected %s to be mentioned in output, got:\n%s", name, output)
		}
	}
}
