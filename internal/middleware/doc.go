// Package middleware is the inbound application layer in VibeWarden's hexagonal
// architecture. It contains all HTTP middleware that sits between the reverse
// proxy (Caddy) and the application services (internal/app).
//
// # Architectural role
//
// In hexagonal architecture the inbound side (also called the "driving side")
// consists of adapters that translate external signals — in this case incoming
// HTTP requests — into calls that exercise the domain through application
// services. This package is that inbound adapter layer for HTTP traffic.
//
//	┌─────────────────────────────────────────────────┐
//	│                  Caddy (reverse proxy)           │
//	└────────────────────────┬────────────────────────┘
//	                         │ HTTP request
//	┌────────────────────────▼────────────────────────┐
//	│          internal/middleware  (this package)     │
//	│   inbound application layer — drives the domain  │
//	│                                                  │
//	│  auth · rate-limit · WAF · security-headers …    │
//	└────────────────────────┬────────────────────────┘
//	                         │ calls ports (interfaces)
//	┌────────────────────────▼────────────────────────┐
//	│               internal/app  (use cases)          │
//	└────────────────────────┬────────────────────────┘
//	                         │
//	┌────────────────────────▼────────────────────────┐
//	│              internal/domain  (business logic)   │
//	└─────────────────────────────────────────────────┘
//
// # Dependency rules
//
// Middleware constructors accept their dependencies through the port interfaces
// defined in internal/ports — never concrete adapter types. This keeps the
// middleware layer decoupled from infrastructure (Postgres, Redis, Kratos …).
//
// The middleware package must not import internal/adapters/* directly. Any
// outbound I/O required by a middleware (e.g. persisting an audit event,
// checking rate-limit counters) must go through a port interface.
//
// # Middleware registration
//
// Each middleware is a standard [net/http] handler wrapper of type
// [github.com/vibewarden/vibewarden/internal/ports.Middleware]. Middlewares are
// assembled into a [github.com/vibewarden/vibewarden/internal/ports.MiddlewareChain]
// and applied to the Caddy handler during plugin initialisation in
// internal/plugins.
package middleware
