package ops

// FormatProjectRootMismatchForTest exposes the unexported
// formatProjectRootMismatch helper so that tests in the _test package can
// assert the exact error message wording (golden tests, ADR-100).
func FormatProjectRootMismatchForTest(tag, currentProjectRoot string, identity ImageIdentity) error {
	return formatProjectRootMismatch(tag, currentProjectRoot, identity)
}

// SetGOOSForTest swaps the package-level goosFn to return the provided goos
// string and returns a restore function that reverts the swap. Callers must
// register the restore function via t.Cleanup to avoid leaking state across
// tests.
//
// This helper exists solely for testability of the darwin-only advisory path
// on non-darwin CI runners. Do not use it in production code.
func SetGOOSForTest(goos string) (restore func()) {
	prev := goosFn
	goosFn = func() string { return goos }
	return func() { goosFn = prev }
}
