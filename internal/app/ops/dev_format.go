package ops

import "fmt"

// formatProjectRootMismatch returns the actionable error message shown to the
// user when the app image's project-root identity does not match the current
// project. Two variants are returned depending on whether the image carries any
// vibew project-root labels at all.
//
// Variant 1 (labelled but different project):
//
//	Error: app image <tag> was built from a different project.
//	  Built from: <other-project-root>
//	  Current:    <current-project-root>
//
//	Rebuild with: vibew dev --rebuild
//
// Variant 2 (unlabelled — legacy image or foreign builder):
//
//	Error: app image <tag> is missing the vibew project-root label.
//	  This image was built before VibeWarden v0.19.0 OR by something other than vibew build.
//	  Current project: <current-project-root>
//
//	Rebuild with: vibew dev --rebuild
//
// The wording is pinned by golden tests in dev_format_test.go. Do not change
// without updating the tests.
//
// Release-ordering note: the recovery command "vibew dev --rebuild" is
// delivered by issue #1220 in the same release cycle (v0.18.3). Both #1219
// and #1220 MUST merge before tagging v0.18.3 — this is the merge-ordering
// invariant for this retro batch and is enforced as a release-gate, not in
// code. If you ever need to ship #1219 ahead of #1220, replace the recovery
// command in both Variant 1 and Variant 2 with the fallback wording:
//
//	vibew down && docker rmi <image> && vibew build && vibew dev
//
// and update the corresponding golden tests in dev_format_test.go.
func formatProjectRootMismatch(tag, currentProjectRoot string, identity ImageIdentity) error {
	var msg string
	if identity.IsLabelled() {
		// Variant 1: image has a label but it points at a different project root.
		msg = fmt.Sprintf(
			"Error: app image %s was built from a different project.\n"+
				"  Built from: %s\n"+
				"  Current:    %s\n"+
				"\n"+
				"Rebuild with: vibew dev --rebuild",
			tag, identity.Path, currentProjectRoot,
		)
	} else {
		// Variant 2: image has no vibew project-root labels at all.
		msg = fmt.Sprintf(
			"Error: app image %s is missing the vibew project-root label.\n"+
				"  This image was built before VibeWarden v0.19.0 OR by something other than vibew build.\n"+
				"  Current project: %s\n"+
				"\n"+
				"Rebuild with: vibew dev --rebuild",
			tag, currentProjectRoot,
		)
	}
	return fmt.Errorf("%s", msg) //nolint:err113 // dynamic user-facing error message; wording is golden-tested
}
