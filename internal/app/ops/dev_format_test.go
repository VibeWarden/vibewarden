package ops_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// TestFormatProjectRootMismatch_Variant1_LabelledButDifferentProject verifies
// the byte-exact wording for the "labelled but wrong project" case.
// This is the golden test pinned by ADR-100.
func TestFormatProjectRootMismatch_Variant1_LabelledButDifferentProject(t *testing.T) {
	identity := ops.ImageIdentity{
		Hash: "sha256:abc123",
		Path: "/Users/foo/old-project",
	}

	err := ops.FormatProjectRootMismatchForTest("qr-code-blackhole-app:latest", "/Users/tibtof/qr-code-blackhole", identity)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	want := "Error: app image qr-code-blackhole-app:latest was built from a different project.\n" +
		"  Built from: /Users/foo/old-project\n" +
		"  Current:    /Users/tibtof/qr-code-blackhole\n" +
		"\n" +
		"Rebuild with: vibew dev --rebuild"

	got := err.Error()
	if got != want {
		t.Errorf("error message mismatch.\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

// TestFormatProjectRootMismatch_Variant2_Unlabelled verifies the byte-exact
// wording for the "unlabelled (legacy or foreign builder)" case.
// This is the golden test pinned by ADR-100.
func TestFormatProjectRootMismatch_Variant2_Unlabelled(t *testing.T) {
	// Zero ImageIdentity — no labels.
	identity := ops.ImageIdentity{}

	err := ops.FormatProjectRootMismatchForTest("qr-code-blackhole-app:latest", "/Users/tibtof/qr-code-blackhole", identity)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	want := "Error: app image qr-code-blackhole-app:latest is missing the vibew project-root label.\n" +
		"  This image was built before VibeWarden v0.19.0 OR by something other than vibew build.\n" +
		"  Current project: /Users/tibtof/qr-code-blackhole\n" +
		"\n" +
		"Rebuild with: vibew dev --rebuild"

	got := err.Error()
	if got != want {
		t.Errorf("error message mismatch.\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

// TestFormatProjectRootMismatch_TagWithColonAndSlash verifies that a tag
// containing colons and slashes renders correctly in both message variants.
func TestFormatProjectRootMismatch_TagWithColonAndSlash(t *testing.T) {
	tag := "ghcr.io/foo/bar:v1"

	t.Run("variant 1 labelled", func(t *testing.T) {
		identity := ops.ImageIdentity{
			Hash: "sha256:deadbeef",
			Path: "/tmp/old",
		}
		err := ops.FormatProjectRootMismatchForTest(tag, "/tmp/new", identity)
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), tag) {
			t.Errorf("error does not contain tag %q:\n%s", tag, err.Error())
		}
		if !strings.Contains(err.Error(), "was built from a different project") {
			t.Errorf("error does not contain variant-1 wording:\n%s", err.Error())
		}
	})

	t.Run("variant 2 unlabelled", func(t *testing.T) {
		err := ops.FormatProjectRootMismatchForTest(tag, "/tmp/new", ops.ImageIdentity{})
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), tag) {
			t.Errorf("error does not contain tag %q:\n%s", tag, err.Error())
		}
		if !strings.Contains(err.Error(), "is missing the vibew project-root label") {
			t.Errorf("error does not contain variant-2 wording:\n%s", err.Error())
		}
	})
}

// TestFormatProjectRootMismatch_AlwaysContainsRebuildHint verifies that both
// message variants always end with the recovery command.
func TestFormatProjectRootMismatch_AlwaysContainsRebuildHint(t *testing.T) {
	tests := []struct {
		name     string
		identity ops.ImageIdentity
	}{
		{
			name:     "labelled mismatch",
			identity: ops.ImageIdentity{Hash: "sha256:abc", Path: "/old"},
		},
		{
			name:     "unlabelled",
			identity: ops.ImageIdentity{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ops.FormatProjectRootMismatchForTest("myapp:latest", "/current", tt.identity)
			if err == nil {
				t.Fatal("expected non-nil error")
			}
			if !strings.Contains(err.Error(), "vibew dev --rebuild") {
				t.Errorf("error does not contain rebuild hint:\n%s", err.Error())
			}
		})
	}
}

// TestFormatProjectRootMismatch_IsNotErrProjectRootMismatch verifies that the
// returned error is a user-facing message error, not the sentinel itself. The
// sentinel is used for errors.Is detection; the formatted error is what the
// user sees.
func TestFormatProjectRootMismatch_IsNotErrProjectRootMismatch(t *testing.T) {
	err := ops.FormatProjectRootMismatchForTest("tag:latest", "/root", ops.ImageIdentity{})
	if errors.Is(err, ops.ErrProjectRootMismatch) {
		t.Error("formatted error should not be the sentinel ErrProjectRootMismatch")
	}
}
