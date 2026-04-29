package ops

// FormatProjectRootMismatchForTest exposes the unexported
// formatProjectRootMismatch helper so that tests in the _test package can
// assert the exact error message wording (golden tests, ADR-100).
func FormatProjectRootMismatchForTest(tag, currentProjectRoot string, identity ImageIdentity) error {
	return formatProjectRootMismatch(tag, currentProjectRoot, identity)
}
