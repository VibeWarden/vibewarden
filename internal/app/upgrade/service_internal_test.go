package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubHTTPClient is a ports.HTTPClient that returns a pre-programmed response
// (or a transport error) for every request.
type stubHTTPClient struct {
	status int
	body   string
	err    error
}

func (s *stubHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

// writeFile writes content to path and fails the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}

// tarGzArchive builds an in-memory .tar.gz containing the supplied
// name -> content entries, in the order given.
func tarGzArchive(t *testing.T, entries [][2]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e[0], Mode: 0o755, Size: int64(len(e[1]))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header %q: %v", e[0], err)
		}
		if _, err := io.WriteString(tw, e[1]); err != nil {
			t.Fatalf("writing tar content %q: %v", e[0], err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}
	return buf.Bytes()
}

func TestAtomicReplace(t *testing.T) {
	t.Run("replaces existing destination and applies mode", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dest := filepath.Join(dir, "dest")
		writeFile(t, src, "new binary")
		writeFile(t, dest, "old binary")

		if err := atomicReplace(src, dest, 0o755); err != nil {
			t.Fatalf("atomicReplace: %v", err)
		}

		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading dest: %v", err)
		}
		if string(got) != "new binary" {
			t.Errorf("dest content = %q, want %q", got, "new binary")
		}
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("stat dest: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("dest mode = %v, want %v", info.Mode().Perm(), os.FileMode(0o755))
		}
	})

	t.Run("creates destination when absent", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dest := filepath.Join(dir, "dest")
		writeFile(t, src, "fresh")

		if err := atomicReplace(src, dest, 0o755); err != nil {
			t.Fatalf("atomicReplace: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading dest: %v", err)
		}
		if string(got) != "fresh" {
			t.Errorf("dest content = %q, want %q", got, "fresh")
		}
	})

	t.Run("missing source leaves no temp file behind", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "dest")
		writeFile(t, dest, "untouched")

		err := atomicReplace(filepath.Join(dir, "does-not-exist"), dest, 0o755)
		if err == nil {
			t.Fatal("atomicReplace with missing source: want error, got nil")
		}
		if !strings.Contains(err.Error(), "opening source") {
			t.Errorf("error = %v, want it to mention opening source", err)
		}

		// The destination must be untouched and the temp file cleaned up.
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading dest: %v", err)
		}
		if string(got) != "untouched" {
			t.Errorf("dest content = %q, want it unchanged", got)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("reading temp dir: %v", err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".vibew-upgrade-") {
				t.Errorf("temp file %q was left behind", e.Name())
			}
		}
	})

	t.Run("missing destination directory fails on temp creation", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		writeFile(t, src, "content")

		err := atomicReplace(src, filepath.Join(dir, "nope", "dest"), 0o755)
		if err == nil {
			t.Fatal("atomicReplace into missing directory: want error, got nil")
		}
		if !strings.Contains(err.Error(), "creating temp file") {
			t.Errorf("error = %v, want it to mention creating temp file", err)
		}
	})
}

func TestInstallBinary(t *testing.T) {
	t.Run("installs without invoking sudo", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		dest := filepath.Join(dir, "dest")
		writeFile(t, src, "binary")

		var out bytes.Buffer
		if err := installBinary(src, dest, 0o755, &out); err != nil {
			t.Fatalf("installBinary: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading dest: %v", err)
		}
		if string(got) != "binary" {
			t.Errorf("dest content = %q, want %q", got, "binary")
		}
		if out.Len() != 0 {
			t.Errorf("unexpected output on the happy path: %q", out.String())
		}
	})

	t.Run("non-permission failure is returned without a sudo retry", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "dest")

		var out bytes.Buffer
		err := installBinary(filepath.Join(dir, "missing"), dest, 0o755, &out)
		if err == nil {
			t.Fatal("installBinary with missing source: want error, got nil")
		}
		if strings.Contains(out.String(), "sudo") {
			t.Errorf("sudo retry attempted for a non-permission error: %q", out.String())
		}
		if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
			t.Errorf("dest was created despite the failure")
		}
	})
}

// TestIsPermissionErr guards the unwrapping behaviour that makes the sudo
// retry in installBinary reachable: atomicReplace wraps every error it returns
// with fmt.Errorf, and os.IsPermission does not unwrap.
func TestIsPermissionErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("boom"), false},
		{"not-exist", fs.ErrNotExist, false},
		{"bare permission", fs.ErrPermission, true},
		{"wrapped permission", fmt.Errorf("creating temp file in %q: %w", "/usr/local/bin", fs.ErrPermission), true},
		{
			"wrapped path error",
			fmt.Errorf("creating temp file: %w", &os.PathError{Op: "open", Path: "/usr/local/bin/x", Err: fs.ErrPermission}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermissionErr(tt.err); got != tt.want {
				t.Errorf("isPermissionErr(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestExtractTarGz(t *testing.T) {
	t.Run("extracts the target and ignores other entries", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "release.tar.gz")
		writeFile(t, archive, "")
		data := tarGzArchive(t, [][2]string{
			{"README.md", "docs"},
			{"LICENSE", "apache"},
			{"vibew", "the binary"},
		})
		if err := os.WriteFile(archive, data, 0o600); err != nil {
			t.Fatalf("writing archive: %v", err)
		}

		destDir := t.TempDir()
		if err := extractTarGz(archive, destDir, "vibew"); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(destDir, "vibew"))
		if err != nil {
			t.Fatalf("reading extracted binary: %v", err)
		}
		if string(got) != "the binary" {
			t.Errorf("extracted content = %q, want %q", got, "the binary")
		}
		if _, err := os.Stat(filepath.Join(destDir, "README.md")); !os.IsNotExist(err) {
			t.Errorf("non-target entry README.md was extracted")
		}
	})

	t.Run("nested target path is flattened into destDir", func(t *testing.T) {
		dir := t.TempDir()
		archive := filepath.Join(dir, "release.tar.gz")
		data := tarGzArchive(t, [][2]string{{"../../etc/vibew", "traversal attempt"}})
		if err := os.WriteFile(archive, data, 0o600); err != nil {
			t.Fatalf("writing archive: %v", err)
		}

		destDir := t.TempDir()
		if err := extractTarGz(archive, destDir, "vibew"); err != nil {
			t.Fatalf("extractTarGz: %v", err)
		}
		// The entry name is only ever used via filepath.Base, so the write
		// lands inside destDir and cannot escape it.
		if _, err := os.Stat(filepath.Join(destDir, "vibew")); err != nil {
			t.Errorf("expected the entry to be written inside destDir: %v", err)
		}
	})

	t.Run("errors", func(t *testing.T) {
		dir := t.TempDir()

		goodArchive := filepath.Join(dir, "good.tar.gz")
		if err := os.WriteFile(goodArchive, tarGzArchive(t, [][2]string{{"other", "x"}}), 0o600); err != nil {
			t.Fatalf("writing archive: %v", err)
		}

		notGzip := filepath.Join(dir, "plain.tar.gz")
		writeFile(t, notGzip, "this is not gzip")

		truncated := filepath.Join(dir, "truncated.tar.gz")
		full := tarGzArchive(t, [][2]string{{"vibew", strings.Repeat("x", 4096)}})
		if err := os.WriteFile(truncated, full[:len(full)/2], 0o600); err != nil {
			t.Fatalf("writing truncated archive: %v", err)
		}

		tests := []struct {
			name        string
			archivePath string
			wantErr     string
		}{
			{"missing archive", filepath.Join(dir, "absent.tar.gz"), "opening archive"},
			{"not a gzip stream", notGzip, "creating gzip reader"},
			{"target not in archive", goodArchive, "not found in archive"},
			{"truncated stream", truncated, "reading tar entry"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := extractTarGz(tt.archivePath, t.TempDir(), "vibew")
				if err == nil {
					t.Fatalf("extractTarGz(%q): want error, got nil", tt.archivePath)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			})
		}
	})

	t.Run("unwritable destination", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root: directory permissions are not enforced")
		}
		dir := t.TempDir()
		archive := filepath.Join(dir, "release.tar.gz")
		if err := os.WriteFile(archive, tarGzArchive(t, [][2]string{{"vibew", "bin"}}), 0o600); err != nil {
			t.Fatalf("writing archive: %v", err)
		}

		destDir := filepath.Join(dir, "readonly")
		if err := os.Mkdir(destDir, 0o500); err != nil {
			t.Fatalf("creating read-only dir: %v", err)
		}
		// G302 flags 0o700 as too permissive for a file; this is a directory,
		// which needs the execute bit for t.TempDir cleanup to remove it.
		t.Cleanup(func() { _ = os.Chmod(destDir, 0o700) }) //nolint:gosec // directory, not a file

		err := extractTarGz(archive, destDir, "vibew")
		if err == nil {
			t.Fatal("extractTarGz into a read-only directory: want error, got nil")
		}
		if !strings.Contains(err.Error(), "creating") {
			t.Errorf("error = %v, want it to mention creating the destination file", err)
		}
	})
}

func TestServiceDownloadFile(t *testing.T) {
	t.Run("writes the response body to dest", func(t *testing.T) {
		s := &Service{http: &stubHTTPClient{status: http.StatusOK, body: "payload"}}
		dest := filepath.Join(t.TempDir(), "out.bin")

		if err := s.downloadFile(context.Background(), "https://example.test/a.tar.gz", dest); err != nil {
			t.Fatalf("downloadFile: %v", err)
		}
		got, err := os.ReadFile(dest)
		if err != nil {
			t.Fatalf("reading dest: %v", err)
		}
		if string(got) != "payload" {
			t.Errorf("dest content = %q, want %q", got, "payload")
		}
	})

	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name    string
			client  *stubHTTPClient
			url     string
			dest    func(t *testing.T) string
			wantErr string
		}{
			{
				name:    "invalid url",
				client:  &stubHTTPClient{status: http.StatusOK, body: "x"},
				url:     "https://example.test/\x7f",
				dest:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "out.bin") },
				wantErr: "creating request",
			},
			{
				name:    "transport error",
				client:  &stubHTTPClient{err: errors.New("dial tcp: connection refused")},
				url:     "https://example.test/a.tar.gz",
				dest:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "out.bin") },
				wantErr: "connection refused",
			},
			{
				name:    "non-200 status",
				client:  &stubHTTPClient{status: http.StatusNotFound, body: "nope"},
				url:     "https://example.test/a.tar.gz",
				dest:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "out.bin") },
				wantErr: "HTTP 404",
			},
			{
				name:    "undirectable destination",
				client:  &stubHTTPClient{status: http.StatusOK, body: "payload"},
				url:     "https://example.test/a.tar.gz",
				dest:    func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing", "out.bin") },
				wantErr: "creating",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s := &Service{http: tt.client}
				err := s.downloadFile(context.Background(), tt.url, tt.dest(t))
				if err == nil {
					t.Fatal("downloadFile: want error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			})
		}
	})
}

func TestVerifyChecksumUnreadableTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "vibew.tar.gz")
	checksums := filepath.Join(dir, "checksums.txt")
	writeFile(t, checksums, "deadbeef  vibew.tar.gz\n")

	// The checksums file lists the target, but the target does not exist.
	err := verifyChecksum(target, checksums)
	if err == nil {
		t.Fatal("verifyChecksum with missing target: want error, got nil")
	}
	if !strings.Contains(err.Error(), "opening") {
		t.Errorf("error = %v, want it to mention opening the target", err)
	}
}

// writeFakeSudo creates an executable named "sudo" in a temp directory,
// prepends that directory to PATH, and returns the path to the file the stub
// writes its arguments to. It lets the sudo-retry branch of installBinary run
// without ever escalating privileges.
func writeFakeSudo(t *testing.T, exitCode int, stderr string) string {
	t.Helper()

	binDir := t.TempDir()
	argsFile := filepath.Join(binDir, "args.txt")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" > %q\nprintf '%%s' %q >&2\nexit %d\n",
		argsFile, stderr, exitCode)
	sudoPath := filepath.Join(binDir, "sudo")
	if err := os.WriteFile(sudoPath, []byte(script), 0o700); err != nil {
		t.Fatalf("writing fake sudo: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsFile
}

// TestInstallBinary_SudoRetry covers the read-only-install-dir path that
// installBinary documents: a permission failure from atomicReplace must be
// retried through `sudo install`. A stub sudo on PATH stands in for the real
// one, so no privilege escalation happens during the test.
func TestInstallBinary_SudoRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sudo retry is not attempted on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}

	// readOnlyDest returns a destination path inside a directory that the test
	// user cannot write to, so atomicReplace fails with a permission error.
	readOnlyDest := func(t *testing.T) (src, dest string) {
		t.Helper()
		dir := t.TempDir()
		src = filepath.Join(dir, "src")
		writeFile(t, src, "binary")

		destDir := filepath.Join(dir, "readonly")
		if err := os.Mkdir(destDir, 0o500); err != nil {
			t.Fatalf("creating read-only dir: %v", err)
		}
		// G302 flags 0o700 as too permissive for a file; this is a directory,
		// which needs the execute bit for t.TempDir cleanup to remove it.
		t.Cleanup(func() { _ = os.Chmod(destDir, 0o700) }) //nolint:gosec // directory, not a file
		return src, filepath.Join(destDir, "vibew")
	}

	t.Run("succeeds via sudo install", func(t *testing.T) {
		argsFile := writeFakeSudo(t, 0, "")
		src, dest := readOnlyDest(t)

		var out bytes.Buffer
		if err := installBinary(src, dest, 0o755, &out); err != nil {
			t.Fatalf("installBinary: %v", err)
		}
		if !strings.Contains(out.String(), "retrying with sudo") {
			t.Errorf("output = %q, want the sudo retry notice", out.String())
		}
		gotArgs, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("fake sudo was never invoked: %v", err)
		}
		want := fmt.Sprintf("install -m 0755 %s %s", src, dest)
		if strings.TrimSpace(string(gotArgs)) != want {
			t.Errorf("sudo args = %q, want %q", strings.TrimSpace(string(gotArgs)), want)
		}
	})

	t.Run("surfaces sudo stderr on failure", func(t *testing.T) {
		writeFakeSudo(t, 1, "install: permission denied")
		src, dest := readOnlyDest(t)

		var out bytes.Buffer
		err := installBinary(src, dest, 0o755, &out)
		if err == nil {
			t.Fatal("installBinary with failing sudo: want error, got nil")
		}
		if !strings.Contains(err.Error(), "sudo install failed") {
			t.Errorf("error = %v, want it to mention the sudo install failure", err)
		}
		if !strings.Contains(err.Error(), "install: permission denied") {
			t.Errorf("error = %v, want it to include sudo stderr", err)
		}
	})
}

// TestAtomicReplace_CopyAndRenameFailures covers the two mid-flight failure
// points that leave the temp file to clean up.
func TestAtomicReplace_CopyAndRenameFailures(t *testing.T) {
	t.Run("source is a directory", func(t *testing.T) {
		dir := t.TempDir()
		srcDir := filepath.Join(dir, "srcdir")
		if err := os.Mkdir(srcDir, 0o755); err != nil {
			t.Fatalf("creating source dir: %v", err)
		}

		err := atomicReplace(srcDir, filepath.Join(dir, "dest"), 0o755)
		if err == nil {
			t.Fatal("atomicReplace from a directory: want error, got nil")
		}
	})

	t.Run("destination is a directory", func(t *testing.T) {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		writeFile(t, src, "content")
		destDir := filepath.Join(dir, "dest")
		if err := os.Mkdir(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir: %v", err)
		}

		err := atomicReplace(src, destDir, 0o755)
		if err == nil {
			t.Fatal("atomicReplace onto a directory: want error, got nil")
		}
		if !strings.Contains(err.Error(), "renaming") {
			t.Errorf("error = %v, want it to mention the rename", err)
		}
	})
}
