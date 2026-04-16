//go:build purity
// +build purity

// Package ports advisory test — runs only under `go test -tags=purity`.
//
// This file guards the hexagonal-architecture invariant locked in
// ADR-064: internal/ports/ may import stdlib and internal/domain/* only.
// Imports of internal/config, internal/adapters/*, or internal/app/* would
// re-introduce the leak that ADR-064 removed.
//
// It is behind a build tag because the repository's default make check
// gate (go build + golangci-lint + go test) does not run this assertion;
// operators or CI can opt in with `go test -tags=purity ./internal/ports/...`
// when an extra guardrail is desirable.

package ports_test

import (
	"go/build"
	"strings"
	"testing"
)

func TestPortsPackage_OnlyImportsStdlibAndDomain(t *testing.T) {
	pkg, err := build.Default.Import("github.com/vibewarden/vibewarden/internal/ports", "", 0)
	if err != nil {
		t.Fatalf("importing internal/ports: %v", err)
	}

	const (
		moduleRoot      = "github.com/vibewarden/vibewarden/"
		allowedInternal = moduleRoot + "internal/domain/"
	)

	banned := []string{
		moduleRoot + "internal/config",
		moduleRoot + "internal/adapters",
		moduleRoot + "internal/app",
	}

	// The stdlib is always allowed (Go treats stdlib imports as import paths
	// without a dot in the first element). Module-relative imports must be
	// under internal/domain/.
	for _, imp := range pkg.Imports {
		if !strings.HasPrefix(imp, moduleRoot) {
			continue // stdlib or external module — not checked here
		}
		for _, bad := range banned {
			if strings.HasPrefix(imp, bad) {
				t.Errorf("internal/ports imports %q — forbidden by ADR-064", imp)
			}
		}
		if !strings.HasPrefix(imp, allowedInternal) && imp != moduleRoot+"internal/ports" {
			t.Errorf("internal/ports imports %q — only stdlib and %s*** are allowed",
				imp, allowedInternal)
		}
	}
}
