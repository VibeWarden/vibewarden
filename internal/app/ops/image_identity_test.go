package ops_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeInspector is a test double for ports.ImageInspector.
type fakeInspector struct {
	info ports.ImageInfo
	err  error
}

func (f *fakeInspector) Inspect(_ context.Context, tag string) (ports.ImageInfo, error) {
	if f.err != nil {
		return ports.ImageInfo{}, f.err
	}
	info := f.info
	info.Tag = tag
	return info, nil
}

// --- ProjectRootHash tests ---

func TestProjectRootHash_ProducesStableOutput(t *testing.T) {
	dir := t.TempDir()

	hashLabel1, pathLabel1, err1 := ops.ProjectRootHash(dir)
	hashLabel2, pathLabel2, err2 := ops.ProjectRootHash(dir)

	if err1 != nil {
		t.Fatalf("first ProjectRootHash() error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second ProjectRootHash() error: %v", err2)
	}
	if hashLabel1 != hashLabel2 {
		t.Errorf("hash not stable: %q != %q", hashLabel1, hashLabel2)
	}
	if pathLabel1 != pathLabel2 {
		t.Errorf("path not stable: %q != %q", pathLabel1, pathLabel2)
	}
}

func TestProjectRootHash_FormatIsSha256Hex(t *testing.T) {
	dir := t.TempDir()

	hashLabel, _, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash() error: %v", err)
	}

	if !strings.HasPrefix(hashLabel, "sha256:") {
		t.Errorf("hashLabel %q must start with 'sha256:'", hashLabel)
	}
	hexPart := strings.TrimPrefix(hashLabel, "sha256:")
	if len(hexPart) != 64 {
		t.Errorf("hex part length = %d, want 64", len(hexPart))
	}
	for _, ch := range hexPart {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			t.Errorf("hex part contains non-lowercase-hex character %q", ch)
			break
		}
	}
}

func TestProjectRootHash_DifferentDirectoriesProduceDifferentHashes(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	hash1, _, err := ops.ProjectRootHash(dir1)
	if err != nil {
		t.Fatalf("ProjectRootHash(dir1) error: %v", err)
	}
	hash2, _, err := ops.ProjectRootHash(dir2)
	if err != nil {
		t.Fatalf("ProjectRootHash(dir2) error: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("different directories produced the same hash: %q", hash1)
	}
}

func TestProjectRootHash_SymlinkProducesIdenticalHash(t *testing.T) {
	real := t.TempDir()

	// Create a symlink pointing at the real directory.
	link := filepath.Join(t.TempDir(), "symlink-to-real")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	hashReal, _, err := ops.ProjectRootHash(real)
	if err != nil {
		t.Fatalf("ProjectRootHash(real) error: %v", err)
	}
	hashLink, _, err := ops.ProjectRootHash(link)
	if err != nil {
		t.Fatalf("ProjectRootHash(link) error: %v", err)
	}

	if hashReal != hashLink {
		t.Errorf("symlink and real dir produced different hashes:\n  real: %q\n  link: %q", hashReal, hashLink)
	}
}

func TestProjectRootHash_PathLabelIsAbsolute(t *testing.T) {
	dir := t.TempDir()

	_, pathLabel, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash() error: %v", err)
	}

	if !filepath.IsAbs(pathLabel) {
		t.Errorf("pathLabel %q should be an absolute path", pathLabel)
	}
}

// --- VerifyAppImageIdentity tests ---

func TestVerifyAppImageIdentity_MatchingHash_ReturnsNil(t *testing.T) {
	dir := t.TempDir()

	expectedHash, expectedPath, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash() error: %v", err)
	}

	fi := &fakeInspector{
		info: ports.ImageInfo{
			Labels: map[string]string{
				ops.LabelProjectRootHash: expectedHash,
				ops.LabelProjectRoot:     expectedPath,
			},
		},
	}

	identity, err := ops.VerifyAppImageIdentity(context.Background(), fi, "myapp:latest", expectedHash)
	if err != nil {
		t.Errorf("VerifyAppImageIdentity() error = %v, want nil", err)
	}
	if identity.Hash != expectedHash {
		t.Errorf("identity.Hash = %q, want %q", identity.Hash, expectedHash)
	}
}

func TestVerifyAppImageIdentity_MismatchedHash_ReturnsErrProjectRootMismatch(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()

	currentHash, _, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash(dir) error: %v", err)
	}
	otherHash, otherPath, err := ops.ProjectRootHash(other)
	if err != nil {
		t.Fatalf("ProjectRootHash(other) error: %v", err)
	}

	fi := &fakeInspector{
		info: ports.ImageInfo{
			Labels: map[string]string{
				ops.LabelProjectRootHash: otherHash,
				ops.LabelProjectRoot:     otherPath,
			},
		},
	}

	identity, err := ops.VerifyAppImageIdentity(context.Background(), fi, "myapp:latest", currentHash)
	if !errors.Is(err, ops.ErrProjectRootMismatch) {
		t.Errorf("VerifyAppImageIdentity() error = %v, want ErrProjectRootMismatch", err)
	}
	if !identity.IsLabelled() {
		t.Error("identity should be labelled for mismatched-hash case")
	}
	if identity.Hash != otherHash {
		t.Errorf("identity.Hash = %q, want %q (the other project's hash)", identity.Hash, otherHash)
	}
}

func TestVerifyAppImageIdentity_MissingLabel_ReturnsErrProjectRootMismatch(t *testing.T) {
	dir := t.TempDir()

	expectedHash, _, err := ops.ProjectRootHash(dir)
	if err != nil {
		t.Fatalf("ProjectRootHash() error: %v", err)
	}

	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name:   "nil labels",
			labels: nil,
		},
		{
			name:   "empty labels map",
			labels: map[string]string{},
		},
		{
			name: "labels without the hash key",
			labels: map[string]string{
				"some.other.label": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fi := &fakeInspector{
				info: ports.ImageInfo{Labels: tt.labels},
			}

			identity, err := ops.VerifyAppImageIdentity(context.Background(), fi, "myapp:latest", expectedHash)
			if !errors.Is(err, ops.ErrProjectRootMismatch) {
				t.Errorf("VerifyAppImageIdentity() error = %v, want ErrProjectRootMismatch", err)
			}
			if identity.IsLabelled() {
				t.Error("identity should NOT be labelled when the hash label is absent")
			}
		})
	}
}

func TestVerifyAppImageIdentity_InspectorError_WrapsError(t *testing.T) {
	sentinelErr := errors.New("docker daemon unreachable")
	fi := &fakeInspector{err: sentinelErr}

	_, err := ops.VerifyAppImageIdentity(context.Background(), fi, "myapp:latest", "sha256:anything")
	if err == nil {
		t.Fatal("expected error from inspector, got nil")
	}
	if errors.Is(err, ops.ErrProjectRootMismatch) {
		t.Error("inspector error should not be treated as ErrProjectRootMismatch")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("error should wrap the sentinel: %v", err)
	}
}

// --- ImageIdentity.IsLabelled tests ---

func TestImageIdentity_IsLabelled(t *testing.T) {
	tests := []struct {
		name     string
		identity ops.ImageIdentity
		want     bool
	}{
		{
			name:     "zero value is unlabelled",
			identity: ops.ImageIdentity{},
			want:     false,
		},
		{
			name:     "hash only — labelled",
			identity: ops.ImageIdentity{Hash: "sha256:abc"},
			want:     true,
		},
		{
			name:     "hash and path — labelled",
			identity: ops.ImageIdentity{Hash: "sha256:abc", Path: "/foo/bar"},
			want:     true,
		},
		{
			name:     "path only — not labelled (hash is authoritative)",
			identity: ops.ImageIdentity{Path: "/foo/bar"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.IsLabelled()
			if got != tt.want {
				t.Errorf("IsLabelled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Ensure the sentinel error and label constants are exported correctly.
var _ = ops.ErrProjectRootMismatch
var _ = ops.LabelProjectRootHash
var _ = ops.LabelProjectRoot
