package ops

import "github.com/vibewarden/vibewarden/internal/config"

// KratosRecoveryCommandForTest exposes the shared recovery command constant so
// golden tests can assert both renderings embed the exact same string.
const KratosRecoveryCommandForTest = kratosRecoveryCommand

// KratosDataLossWarningForTest exposes the shared data-loss warning constant so
// golden tests can assert both renderings embed the exact same string.
const KratosDataLossWarningForTest = kratosDataLossWarning

// KratosMigrateServiceNameForTest exposes the kratos-migrate compose service
// name so tests can assert which services are queried for logs.
const KratosMigrateServiceNameForTest = kratosMigrateServiceName

// LocalKratosMigrateServiceForTest exposes the unexported guard that decides
// whether the generated stack contains a vibew-managed kratos-migrate service.
func LocalKratosMigrateServiceForTest(cfg *config.Config) bool {
	return localKratosMigrateService(cfg)
}

// HasKratosDBCredentialMismatchForTest exposes the unexported log-signature
// matcher for the Kratos credential-mismatch diagnostic.
func HasKratosDBCredentialMismatchForTest(logs string) bool {
	return hasKratosDBCredentialMismatch(logs)
}

// KratosCredentialMismatchBlockForTest exposes the multi-line dev error block
// so its wording can be pinned by golden tests.
func KratosCredentialMismatchBlockForTest() string {
	return kratosCredentialMismatchBlock()
}

// KratosCredentialMismatchDetailForTest exposes the single-line doctor detail
// so its wording can be pinned by golden tests.
func KratosCredentialMismatchDetailForTest() string {
	return kratosCredentialMismatchDetail()
}

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
