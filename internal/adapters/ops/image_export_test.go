package ops_test

import (
	"context"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

// TestImageExportAdapter_CancelledContextReturnsError verifies that Save
// returns an error when the context is already cancelled. This confirms the
// adapter respects context cancellation without requiring a real Docker daemon.
func TestImageExportAdapter_CancelledContextReturnsError(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewImageExportAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so docker exits fast

	err := adapter.Save(ctx, "nonexistent:latest", "/tmp/vibewarden-test-export.tar")
	if err == nil {
		t.Fatal("expected an error because context was cancelled before run")
	}
}

// TestImageExportAdapter_ReturnsErrorWhenDockerMissing verifies that Save
// returns an error when docker is not installed.
func TestImageExportAdapter_ReturnsErrorWhenDockerMissing(t *testing.T) {
	if dockerAvailable() {
		t.Skip("docker is available; skipping missing-docker test")
	}

	adapter := opsadapter.NewImageExportAdapter()
	err := adapter.Save(context.Background(), "myapp:latest", "/tmp/vibewarden-test-export.tar")
	if err == nil {
		t.Fatal("expected an error when docker is not available")
	}
}
