// Package crtsh implements ports.CertTransparencyQuerier by calling the
// public crt.sh Certificate Transparency JSON API.
//
// It is used exclusively by the vibew CLI doctor command to run the
// Let's Encrypt rate-limit preflight check (ADR-090). The adapter is never
// imported from internal/plugins/ or internal/adapters/caddy/ — the sidecar
// runtime does not make outbound crt.sh calls.
package crtsh
