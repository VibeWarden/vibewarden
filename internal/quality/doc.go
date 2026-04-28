// Package quality contains repo-wide invariant tests (CI guards): assertions
// about repository health that are easier to express as Go tests than as shell
// scripts. These tests run automatically under `go test ./...` and therefore
// under both `make check` and the CI "Build & Test" job — no separate workflow
// needed.
package quality
