package events

import (
	"fmt"
	"time"
)

// ProxyStartedParams contains the parameters needed to construct a
// proxy.started event.
type ProxyStartedParams struct {
	// ListenAddr is the address the reverse proxy is listening on (e.g. ":8080").
	ListenAddr string

	// UpstreamAddr is the address requests are forwarded to (e.g. "localhost:3000").
	UpstreamAddr string

	// TLSEnabled reports whether TLS termination is active.
	TLSEnabled bool

	// TLSProvider is the TLS certificate provider (e.g. "letsencrypt", "self-signed").
	// Empty when TLSEnabled is false.
	TLSProvider string

	// SecurityHeadersEnabled reports whether the security headers middleware is active.
	SecurityHeadersEnabled bool

	// Version is the VibeWarden binary version string.
	Version string
}

// NewProxyStarted creates a proxy.started event indicating the reverse proxy
// started successfully and is ready to accept connections.
func NewProxyStarted(params ProxyStartedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeProxyStarted,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Reverse proxy listening on %s, forwarding to %s",
			params.ListenAddr, params.UpstreamAddr,
		),
		Payload: map[string]any{
			"listen":                   params.ListenAddr,
			"upstream":                 params.UpstreamAddr,
			"tls_enabled":              params.TLSEnabled,
			"tls_provider":             params.TLSProvider,
			"security_headers_enabled": params.SecurityHeadersEnabled,
			"version":                  params.Version,
		},
	}
}

// ProxyKratosFlowParams contains the parameters needed to construct a
// proxy.kratos_flow event.
type ProxyKratosFlowParams struct {
	// Method is the HTTP method of the request (e.g. "GET", "POST").
	Method string

	// Path is the URL path of the request.
	Path string
}

// NewProxyKratosFlow creates a proxy.kratos_flow event indicating that a
// request was routed to the Ory Kratos self-service flow API.
func NewProxyKratosFlow(params ProxyKratosFlowParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeProxyKratosFlow,
		Timestamp:     time.Now().UTC(),
		AISummary:     "Request proxied to Kratos self-service API",
		Payload: map[string]any{
			"method": params.Method,
			"path":   params.Path,
		},
	}
}
