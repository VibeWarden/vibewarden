package ports

// HealthIdentityHeader is the HTTP response header always emitted by the
// /_vibewarden/health endpoint, regardless of the health.expose_version
// setting. It is the stable ownership marker used to detect a VibeWarden
// sidecar without relying on the version string being present in the body.
//
// It lives in ports (not in an adapter) so that both the caddy adapter
// (which emits it) and the ops adapter (which probes for it in port_owner)
// can reference the same canonical value without one adapter importing
// another — adapters are peers and must not depend on each other.
const HealthIdentityHeader = "X-Vibewarden"

// HealthIdentityHeaderValue is the fixed value of HealthIdentityHeader ("1").
const HealthIdentityHeaderValue = "1"
