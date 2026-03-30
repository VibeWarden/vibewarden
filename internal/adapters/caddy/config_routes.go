package caddy

import (
	"net"
	"net/url"
)

// kratosFlowPaths contains the URL path patterns that must be proxied to
// the Kratos public API instead of the upstream application.
// These paths are the Kratos self-service browser flows and the Ory canonical prefix.
var kratosFlowPaths = []string{
	"/self-service/login/*",
	"/self-service/registration/*",
	"/self-service/logout/*",
	"/self-service/settings/*",
	"/self-service/recovery/*",
	"/self-service/verification/*",
	"/.ory/kratos/public/*",
}

// buildKratosFlowRoute constructs a Caddy route that transparently proxies all
// Kratos self-service flow paths and the Ory canonical prefix to the Kratos
// public API. This route must be placed after the health check route and before
// the catch-all reverse proxy route so that Kratos paths are never forwarded to
// the upstream application.
//
// The kratosPublicURL must be a valid base URL (e.g. "http://127.0.0.1:4433").
// The host:port portion is extracted and used as the Caddy upstream dial address.
func buildKratosFlowRoute(kratosPublicURL string) map[string]any {
	// Convert the full URL to a host:port dial address for Caddy.
	// Caddy's reverse_proxy handler expects "host:port", not a full URL.
	kratosAddr := urlToDialAddr(kratosPublicURL)

	return map[string]any{
		"match": []map[string]any{
			{"path": kratosFlowPaths},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": kratosAddr},
				},
			},
		},
	}
}

// urlToDialAddr extracts the host:port dial address from a full URL string.
// For example "http://127.0.0.1:4433" becomes "127.0.0.1:4433".
// If the URL has no explicit port the scheme default is used: "80" for http,
// "443" for https. Malformed URLs fall back to returning the original string.
func urlToDialAddr(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	host := u.Hostname()
	port := u.Port()

	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}

	return net.JoinHostPort(host, port)
}

// buildAdminRoute constructs a Caddy route that reverse-proxies all requests
// under /_vibewarden/admin/* to the internal admin HTTP server at internalAddr.
// The AdminAuthHandler in the middleware chain has already validated the bearer
// token by the time the request reaches this route, so no additional auth is
// performed here.
//
// The internalAddr must be a host:port string (e.g., "127.0.0.1:9092").
// The full request path is forwarded unchanged; the internal admin server
// handles routes under /_vibewarden/admin/*.
func buildAdminRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/admin/*"}},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}

// buildDocsRoute constructs a Caddy route that reverse-proxies requests to
// /_vibewarden/api/docs to the internal admin HTTP server at internalAddr.
// This endpoint is public — no authentication is required and it must not be
// gated by the AdminAuthHandler. The route is inserted before the catch-all
// proxy route so that the AdminAuth middleware (which lives in the catch-all
// handler chain) never runs for doc requests.
//
// The internalAddr must be a host:port string (e.g., "127.0.0.1:9092").
func buildDocsRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/api/docs"}},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}

// buildMetricsRoute constructs a Caddy route that reverse-proxies requests to
// /_vibewarden/metrics to the internal metrics HTTP server at internalAddr.
// The internal server is started separately (see adapters/metrics.Server) and
// serves the Prometheus handler on a random localhost port.
//
// The internalAddr must be a host:port string (e.g., "127.0.0.1:9091").
//
// A rewrite handler is placed before reverse_proxy to translate the public path
// /_vibewarden/metrics into /metrics, which is the path the internal ServeMux
// listens on.
func buildMetricsRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/metrics"}},
		},
		"handle": []map[string]any{
			// Rewrite /_vibewarden/metrics → /metrics before proxying.
			{
				"handler": "rewrite",
				"uri":     "/metrics",
			},
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}

// buildStaticReadyRoute constructs a Caddy route for /_vibewarden/ready that
// returns a static 503 response body indicating the process is not yet ready.
// This route is used when no internal readiness server address is configured.
// Kubernetes or other orchestration systems will treat the 503 as "not ready"
// and withhold traffic until the dynamic route takes over after a Reload.
func buildStaticReadyRoute() map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/ready"}},
		},
		"handle": []map[string]any{
			{
				"handler": "static_response",
				"headers": map[string][]string{
					"Content-Type": {"application/json"},
				},
				"body":        `{"ready":false,"reason":"starting"}`,
				"status_code": 503,
			},
		},
	}
}

// buildDynamicReadyRoute constructs a Caddy route that reverse-proxies
// /_vibewarden/ready to the internal readiness HTTP server at internalAddr.
// The internal server runs ReadyHandler which performs live plugin health and
// upstream reachability checks.
//
// The internalAddr must be a host:port string (e.g., "127.0.0.1:9093").
// The full request path is forwarded unchanged; the internal readiness server
// handles requests at /_vibewarden/ready.
func buildDynamicReadyRoute(internalAddr string) map[string]any {
	return map[string]any{
		"match": []map[string]any{
			{"path": []string{"/_vibewarden/ready"}},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]any{
					{"dial": internalAddr},
				},
			},
		},
	}
}
