# ADR-081: Auto-detect architecture mismatch during deploy prerequisites

## Status

Accepted

## Context

Building on Apple Silicon (arm64) and deploying to an amd64 server silently
produces a broken deploy. The only clue is a Docker warning about platform
mismatch buried in container logs, followed by "unhealthy" -- nothing tells the
user "you built the wrong arch."

## Decision

Add an architecture compatibility check to `checkRemotePrerequisites` that runs
**only** when a locally-built Docker image is being transferred to the remote
(i.e. `isLocalImage(imageName) && imageExporter != nil`).

The check:
1. Runs `uname -m` on the remote via the existing `RemoteExecutor.Run` port
2. Normalizes both local (`runtime.GOARCH`) and remote arch using a shared
   `archutil.Normalize` function extracted from `doctor.go`
3. Returns an `ArchMismatchError` with a fix-it message when they differ

### Shared utility

A new `internal/archutil` package provides `Normalize(unameMachine string) string`
so that both `doctor.go` and `deploy/arch.go` share the same normalization logic.

### Testability

`Service` gains a `localArch` field (set via `WithLocalArch`) so tests can
override `runtime.GOARCH` without build tags.

### Skip conditions

The arch check is skipped (deploy proceeds) when:
- No local image will be transferred (registry image, no exporter)
- `uname -m` fails on the remote (cannot determine arch)
- Either architecture normalizes to an empty string

## Consequences

- Users get an immediate, actionable error instead of a cryptic health-check
  timeout when deploying arm64 images to amd64 servers
- The fix-it message suggests `vibew build --platform linux/<remote-arch>`
- No new ports or external dependencies introduced
- `normalizeArch` in `doctor.go` becomes a thin wrapper around `archutil.Normalize`
