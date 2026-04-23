// Package tlspreflight provides the application service that orchestrates
// the Let's Encrypt rate-limit preflight check (ADR-090).
//
// The service queries the Certificate Transparency log via a
// ports.CertTransparencyQuerier, counts LE-issued certificates in the last
// 168-hour window, and maps the count to a domain/tlspreflight.Result.
//
// It is used exclusively by the vibew CLI doctor command. The sidecar runtime
// never instantiates this service.
package tlspreflight
