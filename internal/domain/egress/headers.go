package egress

import "net/http"

// HeadersConfig holds per-route header manipulation rules applied by the
// egress proxy before forwarding a request and before returning the response.
//
// All field values are case-insensitive with respect to HTTP header names.
type HeadersConfig struct {
	// InjectHeaders is a static set of headers added to the outbound request
	// before it is forwarded to the upstream. If the header already exists its
	// value is overwritten.
	InjectHeaders map[string]string

	// StripRequestHeaders is the list of request header names removed before the
	// request is forwarded to the upstream. Use this to prevent internal or
	// sensitive headers (e.g. Cookie, Authorization) from leaking upstream.
	StripRequestHeaders []string

	// StripResponseHeaders is the list of response header names removed from the
	// upstream response before it is returned to the calling application. Use
	// this to suppress server fingerprinting headers such as Server or
	// X-Powered-By.
	StripResponseHeaders []string
}

// defaultSensitiveResponseHeaders is the set of response headers that expose
// backend implementation details. They are stripped from every response
// regardless of per-route configuration.
var defaultSensitiveResponseHeaders = []string{
	"Server",
	"X-Powered-By",
}

// ApplyToRequest returns a copy of h with the per-route injection and strip
// rules applied. It never mutates h.
//
// Order of operations:
//  1. Inject configured headers (overwrite existing values).
//  2. Strip configured request headers.
//  3. Always strip X-Inject-Secret.
func (c HeadersConfig) ApplyToRequest(h http.Header) http.Header {
	out := h.Clone()
	for k, v := range c.InjectHeaders {
		out.Set(k, v)
	}
	for _, name := range c.StripRequestHeaders {
		out.Del(name)
	}
	out.Del("X-Inject-Secret")
	return out
}

// ApplyToResponse returns a copy of h with the per-route and default
// sensitive-header strip rules applied. It never mutates h.
//
// Order of operations:
//  1. Strip default sensitive response headers (Server, X-Powered-By).
//  2. Strip per-route configured response headers.
func (c HeadersConfig) ApplyToResponse(h http.Header) http.Header {
	out := h.Clone()
	for _, name := range defaultSensitiveResponseHeaders {
		out.Del(name)
	}
	for _, name := range c.StripResponseHeaders {
		out.Del(name)
	}
	return out
}
