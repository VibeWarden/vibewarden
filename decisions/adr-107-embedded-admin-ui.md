# ADR-107: Embedded user-management admin UI at /_vibewarden/admin/ui

**Date**: 2026-06-11
**Issue**: [#1391](https://github.com/vibewarden/vibewarden/issues/1391)
**Status**: Accepted

---

## Context

The user-management plugin exposes a headless REST API at `/_vibewarden/admin/*`
(contract: `docs/openapi.yaml`). To view or manage users a vibe coder needs
`curl` or a REST client — there is no zero-install visual surface. Issue #1391
adds a browser-based admin UI embedded in the binary, served by the existing
internal admin HTTP server, activated solely by `admin.enabled: true`.

Three design questions were flagged by the PM and are resolved here:
the auth bootstrap (the UI HTML must load even though `/_vibewarden/admin/*` is
token-gated), the CSP interaction, and the asset/build strategy.

## Decision

Serve a hand-authored, vanilla multi-page UI (one HTML page, one CSS file, one
JS file) embedded via `go:embed`, mounted on the existing internal admin HTTP
server at `/_vibewarden/admin/ui/`. The static UI assets are carved out of the
`admin_auth` token gate; all data endpoints remain token-gated. The JS prompts
for the admin token, stores it in `sessionStorage`, and sends it as
`X-Admin-Key` on every API request.

### Bootstrap — carve the UI asset prefix out of the auth gate

The `admin_auth` Caddy handler (`internal/adapters/caddy/admin_auth_handler.go`)
delegates to `middleware.AdminAuthMiddleware`
(`internal/middleware/admin_auth.go`), which gates every path matching
`strings.HasPrefix(r.URL.Path, "/_vibewarden/admin/")`. A browser navigating to
`/_vibewarden/admin/ui` with no token would otherwise receive the 401 JSON body
instead of the token-prompt HTML.

Resolution: add a single public-prefix exception inside the middleware. Requests
under `/_vibewarden/admin/ui` bypass the token check and are forwarded to the
next handler (the internal admin server's UI handler). The exception lives
**inside** the existing `cfg.Enabled` guard, so when `admin.enabled: false` the
UI prefix still returns 404 like every other admin path — the surface is never
disclosed when disabled.

This loosens nothing: the carved-out assets are static markup/CSS/JS containing
no secrets. All data routes (`/_vibewarden/admin/users*`,
`/_vibewarden/admin/events`, `/_vibewarden/config*`,
`/_vibewarden/admin/proposals*`) keep the token gate unchanged. The JS token
prompt is a UX convenience; the Caddy-level `X-Admin-Key` enforcement on data
routes is the real security boundary and is untouched.

The public prefix is `/_vibewarden/admin/ui` (covers both the bare path and the
`/ui/` subtree). The carve-out is hardcoded in the middleware next to the
existing prefix constants — it is not a configurable field, because the UI path
is a fixed public contract.

### CSP — no override; enforce no-inline assets

CSP is disabled by default in this repo (`ContentSecurityPolicy: ""`, see
`internal/config/security_headers.go` / `DefaultSecurityHeadersConfig`). Users
opt in, and the canonical opt-in is `default-src 'self'`. Embedded UI assets are
served same-origin, so `script-src`/`style-src`/`img-src`/`connect-src`/`font-src`
all resolve to `'self'` and pass. The only thing `'self'` blocks is inline code
(inline `<script>`, inline `<style>`, `style="..."`, `on*=` attributes) and
external origins.

Resolution: the UI assets MUST contain no inline code and reference no external
origin. With that contract the UI works under the strictest realistic CSP
(`default-src 'self'`) with no policy change. We deliberately do NOT add a
UI-path-scoped CSP override — weakening CSP for a path would be a regression and
is unnecessary.

### Asset strategy — vanilla MPA, stdlib file server over embed.FS

No Node/npm toolchain is introduced (constraint 2). The UI is hand-authored
vanilla HTML/CSS/JS committed under `internal/plugins/usermgmt/ui/assets/` and
embedded with `go:embed`. Precedent: `internal/adapters/authui` already embeds
HTML templates and talks to Kratos with vanilla JS, no framework.

The Go handler uses only stdlib: `embed.FS` + `io/fs.Sub` + `http.FileServerFS`.
No directory listing (the FS sub-tree handler is wrapped to 404 on directory
requests other than the index). Correct MIME types come from stdlib's extension
mapping (`.html`, `.css`, `.js`). The index response carries
`Cache-Control: no-store`.

It is a multi-page app in the trivial sense — a single `index.html` plus
`app.js` doing client-side view switching (token prompt vs user list). No
client-side router and no history API are required, so no SPA fallback routing
is needed; unknown sub-paths under `/ui/` return 404.

#### File layout

```
internal/plugins/usermgmt/ui/
  embed.go            # //go:embed assets ; exports UIFS and Assets() fs.FS
  embed_test.go       # artifact test: index.html, app.js, styles.css present
  assets/
    index.html        # no inline code; <script src="app.js" defer>, <link styles.css>
    app.js            # token prompt + list/create/deactivate; X-Admin-Key per request
    styles.css        # system font stack; no @import of external fonts
internal/adapters/http/
  admin_ui_handler.go       # AdminUIHandler over http.FileServerFS
  admin_ui_handler_test.go
```

Modified: `internal/middleware/admin_auth.go` (carve-out),
`internal/adapters/http/admin_server.go` (register UI handler on mux),
`CHANGELOG.md`.

### Routing and handler

- `GET /_vibewarden/admin/ui` → 301 to `/_vibewarden/admin/ui/`.
- `GET /_vibewarden/admin/ui/` → `index.html`, 200,
  `Content-Type: text/html; charset=utf-8`, `Cache-Control: no-store`.
- `GET /_vibewarden/admin/ui/app.js`, `/styles.css` → embedded asset, correct MIME.
- Unknown `/_vibewarden/admin/ui/<x>` → 404; directory listing disabled.

The existing reverse-proxy route for `/_vibewarden/admin/*`
(`ContributeCaddyRoutes`, priority 60) already forwards `/ui/*` to the internal
admin server — no new Caddy route is added. Only the auth middleware changes.

### Sequence (first visit)

1. Browser GET `/_vibewarden/admin/ui` (no token).
2. Caddy `admin_auth` handler: path is under the carved-out `/ui` prefix and
   `admin.enabled` is true → bypass token check, forward.
3. Reverse-proxy route forwards to internal admin server.
4. `AdminUIHandler`: 301 → `/ui/`, then serves `index.html` (`no-store`).
5. Browser loads `app.js`, `styles.css` (same path, same carve-out).
6. `app.js`: no token in `sessionStorage` → render token prompt.
7. User enters token → stored under `sessionStorage['vibewarden_admin_token']`.
8. `app.js` GET `/_vibewarden/admin/users` with `X-Admin-Key: <token>`.
9. Caddy `admin_auth`: path NOT under `/ui` → token validated → 200 JSON.
10. On any 401: clear sessionStorage, return to prompt, show error.

### Error cases

- `admin.enabled: false` → `/ui` and all admin paths 404 (existence hidden).
- Missing/invalid token on a data route → 401 JSON (unchanged); JS resets to prompt.
- Asset not found under `/ui/` → 404.
- Embed missing an expected file → caught by the artifact test at build/CI time.

### Test strategy

Unit/handler tests (stdlib `httptest`, no external services):
- `/ui` → 301 `/ui/`; `/ui/` → 200 text/html, `Cache-Control: no-store`.
- asset content-types correct; unknown sub-path 404; no directory listing.
Auth carve-out tests (middleware-level):
- GET `/ui/` with no `X-Admin-Key` → 200 when enabled.
- GET `/_vibewarden/admin/users` with no key → still 401.
- GET `/ui/` when `admin.enabled: false` → 404.
Artifact test:
- embedded FS contains `assets/index.html`, `assets/app.js`, `assets/styles.css`.

### New dependencies

None. stdlib only (`net/http`, `embed`, `io/fs`).

## Consequences

- New public surface in the sidecar: `/_vibewarden/admin/ui/*` is now an
  unauthenticated static-asset path (assets only; data stays gated). The
  carve-out is a fixed contract — renaming `/ui` is a behaviour change.
- Binary grows by the embedded asset size (hand-authored vanilla files, small;
  exact delta documented in the PR per acceptance criterion).
- No build toolchain added; assets are directly editable and reviewable.
- Works under `default-src 'self'` with no CSP weakening, provided the no-inline
  asset contract holds — enforced by review and the no-inline asset convention.
- Localhost-only assumption accepted: no CSRF/rate-limit on the UI route; token
  travels in `X-Admin-Key` header (not a cookie), so there is no CSRF vector.
