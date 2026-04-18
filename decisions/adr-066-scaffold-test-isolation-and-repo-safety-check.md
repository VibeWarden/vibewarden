# ADR-066: Scaffold Test Isolation and Repo Safety Check

**Date**: 2026-04-15
**Issue**: #844
**Status**: Accepted (implemented in PR #846)

### Context

CLI integration tests exercising `vibew init` and `vibew wrap` ran scaffold
commands inside the host repository's working tree, creating spurious git
commits, deleting tracked files, and flipping `core.bare=true` on
`.git/config`. Any contributor running `make check` was one push away from
proposing a PR that deleted the entire repo. This was classified as
CRITICAL after two occurrences in 24 hours (#843, #845).

### Decision

1. **Safety check in the app service layer.** `checkNotInsideGitRepo` in
   `internal/app/scaffold/init_project.go` walks upward from the target
   directory looking for a `.git` directory. If found and the repo has at
   least one commit (`git rev-parse HEAD` succeeds), the command returns
   `ErrInsideExistingGitRepo`. The check is skipped when `--force` is
   passed. Placing the check in the app layer (not CLI) ensures all
   callers get the safety net automatically.

2. **Git environment isolation.** `cleanGitEnv` strips `GIT_DIR`,
   `GIT_WORK_TREE`, and `GIT_CEILING_DIRECTORIES` from the inherited
   environment and sets `GIT_CEILING_DIRECTORIES` to the scaffold target
   directory. Every `exec.Cmd` in `initGitRepo` uses this clean
   environment. This is the root-cause fix: git can no longer discover
   or mutate any parent repository.

3. **Domain sentinel error.** `ErrInsideExistingGitRepo` is defined in
   `internal/domain/scaffold/types.go` as a domain-level sentinel,
   keeping the domain layer free of I/O concerns while giving CLI
   commands a stable error to match on with `errors.Is`.

4. **Regression gate.** `TestScaffoldTests_DoNotPolluteHostRepo` in
   `internal/cli/cmd/scaffold_pollution_test.go` snapshots `HEAD`,
   `git status`, and `core.bare` before and after all scaffold tests
   run, failing loudly if any test mutates the outer repo.

5. **Test isolation helper.** All scaffold tests use `t.TempDir()`
   directories outside the repo tree with `GIT_CEILING_DIRECTORIES`
   set, ensuring no test can reach the host repo.

### Consequences

#### Positive

- `make check` can never pollute the host repository's git state.
- The pre-push hook can be re-enabled (was bypassed with `--no-verify`).
- `vibew init` inside a populated repo gives a clear, actionable error.
- Regression test provides ongoing detection if isolation ever breaks.

#### Negative

- `checkNotInsideGitRepo` shells out to `git rev-parse HEAD`, adding a
  dependency on the git binary at scaffold time. Acceptable because
  `initGitRepo` already requires git.
- Tests that need git context must use the isolation helper, adding a
  small amount of boilerplate per test.
