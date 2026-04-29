// Package architecture_test — image identity label invariant (ADR-100).
//
// This file adds to the repo-wide architecture invariant suite. It guards the
// rule established in ADR-100: BuildService.Run MUST always pass both vibew
// project-root identity label keys to the DockerBuilder port, regardless of
// how the build is invoked (explicit tag, config-derived tag, nil config, etc.).
//
// Why a separate architecture test rather than only unit tests in build_test.go?
// build_test.go covers the current happy paths. This test is the regression
// guard for a NEW build code path (e.g. a fast-rebuild short-circuit introduced
// by #1220) that a future PR might add without realising it must also call
// BuildLabels / ProjectRootHash. That omission would silently drop identity
// stamping, causing "vibew dev" to block every user. The architecture test is
// load-bearing precisely because it will fail the moment a second Build call
// path is introduced without label wiring.
package architecture_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// captureBuilder is a minimal ports.DockerBuilder fake that records every
// Build call so the test can assert the Labels field of DockerBuildOptions.
type captureBuilder struct {
	calls []ports.DockerBuildOptions
}

func (b *captureBuilder) Build(_ context.Context, _ string, _ string, opts ports.DockerBuildOptions) error {
	b.calls = append(b.calls, opts)
	return nil
}

// TestBuildService_Run_AlwaysStampsBothLabelKeys is the architecture invariant
// test for ADR-100 §Test strategy. It asserts that every invocation of
// BuildService.Run — regardless of how the image tag is resolved — passes both
// org.vibewarden.project-root-hash and org.vibewarden.project-root in the
// Labels field of DockerBuildOptions handed to the builder port.
//
// If a future PR introduces a second build code path (e.g. a rebuild fast-path
// in #1220) without calling BuildLabels / ProjectRootHash, this test will fail
// because the captured DockerBuildOptions will have nil or partial Labels.
func TestBuildService_Run_AlwaysStampsBothLabelKeys(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		cfg  *config.Config
		opts ops.BuildOptions
	}{
		{
			name: "config with explicit name and ProjectRoot",
			cfg:  &config.Config{Name: "myapp", ProjectRoot: dir},
			opts: ops.BuildOptions{WorkDir: dir},
		},
		{
			name: "nil config — WorkDir used as project root",
			cfg:  nil,
			opts: ops.BuildOptions{WorkDir: dir},
		},
		{
			name: "pre-resolved ImageTag — label path still runs",
			cfg:  &config.Config{Name: "myapp", ProjectRoot: dir},
			opts: ops.BuildOptions{WorkDir: dir, ImageTag: "myapp-app:latest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := &captureBuilder{}
			svc := ops.NewBuildService(cb)

			var out bytes.Buffer
			if err := svc.Run(context.Background(), tt.cfg, tt.opts, &out); err != nil {
				t.Fatalf("BuildService.Run() error = %v", err)
			}

			if len(cb.calls) != 1 {
				t.Fatalf("expected exactly 1 Build call, got %d", len(cb.calls))
			}
			captured := cb.calls[0]

			// --- Assert both label keys are present ---

			if captured.Labels == nil {
				t.Fatal("DockerBuildOptions.Labels is nil; BuildService.Run must always stamp both identity labels (ADR-100)")
			}

			hashVal, ok := captured.Labels[ops.LabelProjectRootHash]
			if !ok {
				t.Errorf("Labels missing key %q — ADR-100 requires BuildService.Run to always stamp this key; got: %v",
					ops.LabelProjectRootHash, captured.Labels)
			}
			if !strings.HasPrefix(hashVal, "sha256:") {
				t.Errorf("Labels[%q] = %q — value must start with 'sha256:'", ops.LabelProjectRootHash, hashVal)
			}

			if _, ok := captured.Labels[ops.LabelProjectRoot]; !ok {
				t.Errorf("Labels missing key %q — ADR-100 requires BuildService.Run to always stamp this key; got: %v",
					ops.LabelProjectRoot, captured.Labels)
			}
		})
	}
}
