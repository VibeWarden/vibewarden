# ADR-108: No authorization policy engine (Casbin or similar) in the sidecar

**Date**: 2026-09-03
**Issue**: [#1461](https://github.com/vibewarden/vibewarden/issues/1461)
**Status**: Accepted

---

## Context

The question recurs whenever authorization comes up: should VibeWarden embed a general
policy engine — Casbin being the usual candidate — so operators can express arbitrary
access rules instead of the fixed rule shapes the sidecar ships today?

### What the sidecar authorizes today

Three mechanisms, all evaluated on the request path inside the proxy:

1. **`auth.role_paths`** (Kratos mode) — maps a role name to URL path patterns. The
   authenticated identity's `role` trait is compared against the role required for the
   path; a mismatch is 403. Config in `internal/config/auth.go`, matching in
   `AuthHandler.matchRequiredRole` (`internal/adapters/caddy/auth_handler.go`).

2. **`auth.api_key.scope_rules`** (API-key mode) — an ordered list of
   path-glob + method + required-scope rules. `auth.ScopeRule`
   (`internal/domain/auth/scope_rule.go`) is a pure domain value object; the first
   matching rule decides the required scopes, and a key lacking them gets 403.

3. **The admin token gate** — everything under `/_vibewarden/admin/` requires
   `X-Admin-Key`, with a constant-time compare and a 404 (not 401) when admin is
   disabled, so the surface is not disclosed (`internal/middleware/admin_auth.go`).

The union of these is the sidecar's authorization **ceiling**:
*path × method × one identity attribute*. That is the most a reverse proxy can decide
without knowing the application's data. Anything richer — "the owner of *this* document",
"members of *this* tenant", per-record field visibility — requires resource state the
sidecar does not have and must not start replicating. That work belongs in the
application behind the proxy.

### Why a policy engine does not fit

- **It buys nothing at the current ceiling.** Casbin's value is expressing models the
  host does not have: RBAC with domains, ABAC, ReBAC, priority chains. At
  path × method × attribute, `RolePaths` and `ScopeRule` already cover the whole space,
  so the engine would add a DSL and an evaluation loop that decide exactly what a
  20-line matcher decides now.

- **The model DSL contradicts the zero-friction config decision.** Casbin needs a model
  file (`[request_definition]` / `[policy_definition]` / `[matchers]`) plus a policy
  source, in its own syntax, next to `vibewarden.yaml`. VibeWarden's locked plugin model
  is flat top-level keys in one YAML file that a strict loader validates and rejects
  unknown keys in (`internal/config/strict.go`). A second configuration language with
  its own failure modes is precisely the friction the target user was chosen to avoid,
  and `vibew validate` could not meaningfully check it.

- **Silent fail-open matchers are a known hazard class in this codebase.** A policy
  whose matcher never fires does not error; it allows. VibeWarden has already shipped
  that failure twice: `auth.mode: api-key` was a no-op for a release because no handler
  invoked the validator (#1302, ADR-103), and `vibew obs up` silently matched nothing
  (#1176/#1177, ADR-097). A user-authored DSL evaluated at runtime multiplies the
  surface for exactly this bug, in the one place where the failure mode is
  "unauthenticated request served".

- **The dependency graph is already oversized.** `go mod graph` is at ~2,965 edges,
  mostly from the Caddy embed, and reducing it is open work (#1299). Adding a policy
  engine plus its adapters/watchers moves that number the wrong way for a feature with
  no capability gain.

---

## Decision

**VibeWarden does not embed Casbin or any other general-purpose authorization policy
engine.** Authorization in the sidecar stays at path × method × identity attribute,
expressed in `vibewarden.yaml` and evaluated by first-party Go code. Application-level
authorization (ownership, tenancy, per-record rules) is explicitly out of scope for the
sidecar and belongs to the application behind it.

This decision covers the policy-engine dependency, not the feature space: `role_paths`
and `scope_rules` may keep growing within their ceiling.

### Evolution path, if authz needs grow

Ordered by cost, to be taken only under real demand. None of these is scheduled.

1. **Unify `role_paths` and `scope_rules` into one authz rules block** with opt-in
   deny-by-default. Two independent rule shapes for the same decision is the actual
   present-day wart; the fix is consolidation in our own config, not a third syntax.
   Deny-by-default must be opt-in — `ScopeRule` is open-by-default when no rule matches
   (documented on the type), and flipping that silently would lock out live deployments.

2. **CEL as the ABAC escape hatch**, if per-request conditions are ever needed.
   `github.com/google/cel-go` is already in the binary (indirect, via
   `caddy/v2/modules/caddyhttp`), so this costs no new dependency and no new license
   review, and CEL is the expression language Caddy operators already meet.

3. **Evaluate Ory Keto — not Casbin — if relation-based authz gets real demand.** It is
   Apache 2.0, it is the identity stack we already run (Kratos), and ReBAC is a separate
   service with its own storage, not a library linked into the proxy. That keeps the
   sidecar binary and the sidecar's blast radius unchanged.

---

## Consequences

- No code changes. This ADR records a boundary, not a refactor.
- "Should we add Casbin?" is answered by reference to this file. Re-opening it requires
  a use case that is genuinely outside path × method × attribute *and* belongs in the
  proxy rather than the application.
- The ceiling is now written down, so docs and support answers can point at it instead
  of restating it: authorization the sidecar cannot see the data for is the
  application's job.
- Consolidating `role_paths` and `scope_rules` (evolution step 1) stays available and
  is unblocked by this decision.
