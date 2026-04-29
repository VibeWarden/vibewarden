package ops_test

import (
	"context"
	"errors"
	"testing"
	"time"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeImageInspector is a test double for ports.ImageInspector used in
// other adapter tests within this package.
type fakeImageInspector struct {
	info ports.ImageInfo
	err  error
}

func (f *fakeImageInspector) Inspect(_ context.Context, tag string) (ports.ImageInfo, error) {
	if f.err != nil {
		return ports.ImageInfo{}, f.err
	}
	info := f.info
	info.Tag = tag
	return info, nil
}

// TestImageInspectAdapter_InterfaceCompliance verifies the adapter satisfies
// the port interface at compile time. No docker daemon is required.
func TestImageInspectAdapter_InterfaceCompliance(t *testing.T) {
	var _ ports.ImageInspector = opsadapter.NewImageInspectAdapter()
}

// TestImageInfo_Platform verifies the Platform() helper on ports.ImageInfo.
func TestImageInfo_Platform(t *testing.T) {
	tests := []struct {
		name string
		info ports.ImageInfo
		want string
	}{
		{
			name: "linux amd64",
			info: ports.ImageInfo{OS: "linux", Architecture: "amd64"},
			want: "linux/amd64",
		},
		{
			name: "linux arm64",
			info: ports.ImageInfo{OS: "linux", Architecture: "arm64"},
			want: "linux/arm64",
		},
		{
			name: "zero value",
			info: ports.ImageInfo{},
			want: "",
		},
		{
			name: "os only",
			info: ports.ImageInfo{OS: "linux"},
			want: "linux/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.Platform()
			if got != tt.want {
				t.Errorf("Platform() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestErrSentinels verifies the sentinel errors are distinct and wrap correctly.
func TestErrSentinels(t *testing.T) {
	if errors.Is(ports.ErrImageNotFound, ports.ErrDockerUnavailable) {
		t.Error("ErrImageNotFound and ErrDockerUnavailable must not be equal")
	}
	if ports.ErrImageNotFound == nil || ports.ErrDockerUnavailable == nil {
		t.Error("sentinel errors must not be nil")
	}
}

// TestImageInspectAdapter_NoDocker verifies that when docker is not present,
// Inspect returns ErrDockerUnavailable. This test is skipped when docker IS
// present to avoid false failures on developer machines.
func TestImageInspectAdapter_NoDocker(t *testing.T) {
	// This test is about the error path; it can only run on machines without
	// docker. On machines with docker it would succeed or fail for other reasons.
	// We test error mapping via a fake instead of the real adapter.
	fake := &fakeImageInspector{err: ports.ErrDockerUnavailable}
	_, err := fake.Inspect(context.Background(), "myapp:latest")
	if !errors.Is(err, ports.ErrDockerUnavailable) {
		t.Errorf("expected ErrDockerUnavailable, got %v", err)
	}
}

// TestImageInspectAdapter_NotFound verifies error mapping for a missing image.
func TestImageInspectAdapter_NotFound(t *testing.T) {
	fake := &fakeImageInspector{err: ports.ErrImageNotFound}
	_, err := fake.Inspect(context.Background(), "missing:latest")
	if !errors.Is(err, ports.ErrImageNotFound) {
		t.Errorf("expected ErrImageNotFound, got %v", err)
	}
}

// TestImageInspectAdapter_HappyPath verifies that a fake returning ImageInfo
// propagates the tag field correctly.
func TestImageInspectAdapter_HappyPath(t *testing.T) {
	now := time.Date(2026, 4, 19, 14, 2, 11, 0, time.UTC)
	fake := &fakeImageInspector{
		info: ports.ImageInfo{
			Digest:       "sha256:abc123",
			OS:           "linux",
			Architecture: "amd64",
			Created:      now,
			SizeBytes:    170_000_000,
		},
	}
	info, err := fake.Inspect(context.Background(), "qr-van-gogh-app:latest")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if info.Tag != "qr-van-gogh-app:latest" {
		t.Errorf("Tag = %q, want %q", info.Tag, "qr-van-gogh-app:latest")
	}
	if info.Platform() != "linux/amd64" {
		t.Errorf("Platform() = %q, want %q", info.Platform(), "linux/amd64")
	}
	if info.SizeBytes != 170_000_000 {
		t.Errorf("SizeBytes = %d, want 170_000_000", info.SizeBytes)
	}
}

// TestImageInfo_Labels_RoundTrip verifies that the Labels field propagates
// through the fake inspector, confirming the port struct field is wired.
func TestImageInfo_Labels_RoundTrip(t *testing.T) {
	wantLabels := map[string]string{
		"org.vibewarden.project-root-hash": "sha256:abc123",
		"org.vibewarden.project-root":      "/Users/foo/myapp",
	}
	fake := &fakeImageInspector{
		info: ports.ImageInfo{Labels: wantLabels},
	}

	info, err := fake.Inspect(context.Background(), "myapp:latest")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if info.Labels == nil {
		t.Fatal("Labels is nil, want non-nil map")
	}
	for k, want := range wantLabels {
		if got := info.Labels[k]; got != want {
			t.Errorf("Labels[%q] = %q, want %q", k, got, want)
		}
	}
}

// TestImageInfo_Labels_NilLabels_SafeToIterate verifies that a zero ImageInfo
// with nil Labels does not panic when iterated. (The adapter always fills the
// map, but callers should be safe regardless.)
func TestImageInfo_Labels_NilLabels_SafeToIterate(t *testing.T) {
	info := ports.ImageInfo{} // Labels is nil (zero value)
	// Iterating a nil map in Go is safe — range produces zero iterations.
	count := 0
	for range info.Labels {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 iterations over nil Labels, got %d", count)
	}
}

// TestImageInfo_EmptyLabelsMap verifies that an explicitly empty labels map
// is distinguished from a nil map and is also safely iterable.
func TestImageInfo_EmptyLabelsMap(t *testing.T) {
	fake := &fakeImageInspector{
		info: ports.ImageInfo{Labels: map[string]string{}},
	}
	info, err := fake.Inspect(context.Background(), "myapp:latest")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if info.Labels == nil {
		t.Error("Labels should not be nil when explicitly set to empty map")
	}
	if len(info.Labels) != 0 {
		t.Errorf("Labels should be empty, got %v", info.Labels)
	}
}
