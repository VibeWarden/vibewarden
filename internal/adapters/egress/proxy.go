// Package egress implements the HTTP listener and request forwarding adapter
// for the egress proxy plugin. It listens on a dedicated localhost port and
// forwards outbound requests from the wrapped application to external services,
// enforcing the configured allowlist and default policy.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// namedRoutePrefix is the URL path prefix for named-route requests.
	namedRoutePrefix = "/_egress/"

	// headerEgressURL is the request header used in transparent routing mode.
	// The caller sets this header to the full target URL when POSTing to the proxy.
	headerEgressURL = "X-Egress-URL"

	// defaultListen is the default address the egress proxy binds to.
	defaultListen = "127.0.0.1:8081"

	// defaultTimeout is used when the configuration does not specify a timeout.
	defaultTimeout = 30 * time.Second

	// hopByHopHeaders lists headers that must not be forwarded to the upstream.
	// These are connection-specific and must be stripped per RFC 7230 §6.1.
)

// hopByHopHeaders is the set of headers that must not be forwarded upstream.
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailers":            {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// ProxyConfig holds the resolved configuration for the egress proxy listener.
type ProxyConfig struct {
	// Listen is the TCP address to bind the proxy listener to.
	// Defaults to "127.0.0.1:8081".
	Listen string

	// DefaultPolicy is the egress domain policy applied when no route matches.
	DefaultPolicy domainegress.Policy

	// DefaultTimeout is the global request timeout applied when a route does not
	// specify its own timeout.
	DefaultTimeout time.Duration

	// Routes is the ordered list of configured egress routes.
	// Routes are evaluated in declaration order; the first matching route wins.
	Routes []domainegress.Route

	// SSRFGuard, when non-nil, enforces SSRF protection on all outbound
	// connections. It intercepts DialContext calls on the HTTP transport,
	// resolves target hostnames, and blocks requests that resolve to private
	// or reserved IP addresses. When nil, no SSRF protection is applied.
	SSRFGuard *SSRFGuard

	// SecretInjector, when non-nil, is called for routes that have a SecretConfig
	// to fetch and inject the secret value as a request header before forwarding.
	// When nil, no secret injection is performed even if a route is configured
	// with a SecretConfig.
	SecretInjector ports.SecretInjector
}

// Proxy is an HTTP server that listens on a dedicated localhost port and
// forwards outbound requests from the wrapped application to external services.
// It supports two routing styles:
//
//   - Transparent: the caller sets the X-Egress-URL header containing the full
//     target URL and sends the request to any path on the proxy address.
//
//   - Named: the caller addresses /_egress/{route-name}/rest/of/path. The proxy
//     resolves the named route's pattern prefix to build the target URL.
//
// Proxy implements ports.EgressProxy.
type Proxy struct {
	cfg      ProxyConfig
	resolver ports.RouteResolver
	client   *http.Client
	logger   *slog.Logger

	listener net.Listener
	server   *http.Server
}

// secretInjector returns the configured SecretInjector or nil.
func (p *Proxy) secretInjector() ports.SecretInjector {
	return p.cfg.SecretInjector
}

// NewProxy creates a new Proxy from the given configuration, resolver, HTTP
// client, and logger. Pass nil for client to use a default client with
// sensible timeouts. Pass nil for logger to use slog.Default().
func NewProxy(cfg ProxyConfig, resolver ports.RouteResolver, client *http.Client, logger *slog.Logger) *Proxy {
	if cfg.Listen == "" {
		cfg.Listen = defaultListen
	}
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = defaultTimeout
	}
	if cfg.DefaultPolicy == "" {
		cfg.DefaultPolicy = domainegress.PolicyDeny
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if cfg.SSRFGuard != nil {
			transport.DialContext = cfg.SSRFGuard.DialContext
		}
		client = &http.Client{
			Timeout:   cfg.DefaultTimeout,
			Transport: transport,
			// Do not follow redirects automatically — let the caller decide.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Proxy{
		cfg:      cfg,
		resolver: resolver,
		client:   client,
		logger:   logger,
	}
}

// Start binds the TCP listener and begins serving egress requests.
// Start returns immediately; the server continues running until Stop is called.
func (p *Proxy) Start() error {
	ln, err := net.Listen("tcp", p.cfg.Listen)
	if err != nil {
		return fmt.Errorf("binding egress proxy listener on %s: %w", p.cfg.Listen, err)
	}
	p.listener = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleRequest)

	p.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			p.logger.Error("egress proxy stopped unexpectedly", "err", err)
		}
	}()

	p.logger.Info("egress proxy listening", "addr", p.cfg.Listen)
	return nil
}

// Addr returns the address the proxy is listening on.
// Must only be called after a successful Start.
func (p *Proxy) Addr() string {
	if p.listener == nil {
		return ""
	}
	return p.listener.Addr().String()
}

// Stop gracefully shuts down the egress proxy using the provided context.
func (p *Proxy) Stop(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	if err := p.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down egress proxy: %w", err)
	}
	return nil
}

// HandleRequest implements ports.EgressProxy. It resolves the route for the
// request, enforces the default policy, and forwards the request upstream.
func (p *Proxy) HandleRequest(ctx context.Context, req domainegress.EgressRequest) (domainegress.EgressResponse, error) {
	match, err := p.resolver.Resolve(ctx, req)
	if err != nil {
		return domainegress.EgressResponse{}, fmt.Errorf("resolving route: %w", err)
	}

	if !match.Matched {
		if p.cfg.DefaultPolicy == domainegress.PolicyDeny {
			return domainegress.EgressResponse{}, ErrDeniedByPolicy
		}
	}

	return p.forward(ctx, req, match)
}

// handleRequest is the net/http handler registered on the proxy mux. It
// extracts the egress request from the incoming HTTP request, delegates to
// HandleRequest, and writes the upstream response back to the caller.
func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	targetURL, err := p.resolveTargetURL(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("bad request: %s", err), http.StatusBadRequest)
		return
	}

	// Copy the incoming headers, stripping hop-by-hop entries.
	outHeaders := cloneAndStripHopByHop(r.Header)
	// Remove the proxy-specific header from the forwarded request.
	outHeaders.Del(headerEgressURL)

	egressReq, err := domainegress.NewEgressRequest(r.Method, targetURL, outHeaders, r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid egress request: %s", err), http.StatusBadRequest)
		return
	}

	egressResp, err := p.HandleRequest(r.Context(), egressReq)
	if err != nil {
		if err == ErrDeniedByPolicy {
			http.Error(w, "403 Forbidden: request denied by egress policy", http.StatusForbidden)
			return
		}
		var ssrfErr *SSRFBlockedError
		if errors.As(err, &ssrfErr) {
			p.logger.WarnContext(r.Context(), "egress SSRF protection blocked request",
				slog.String("target", targetURL),
				slog.String("host", ssrfErr.Host),
				slog.String("resolved_ip", ssrfErr.IP.String()),
			)
			http.Error(w, "403 Forbidden: "+ssrfErr.Error(), http.StatusForbidden)
			return
		}
		p.logger.ErrorContext(r.Context(), "egress forwarding error",
			slog.String("target", targetURL),
			slog.String("method", r.Method),
			slog.String("err", err.Error()),
		)
		http.Error(w, "egress proxy error", http.StatusBadGateway)
		return
	}

	// Write the upstream response back to the caller.
	respBody, _ := egressResp.BodyRef.(io.ReadCloser)

	for key, vals := range cloneAndStripHopByHop(egressResp.Header) {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(egressResp.StatusCode)

	if respBody != nil {
		defer respBody.Close() //nolint:errcheck
		if _, err := io.Copy(w, respBody); err != nil {
			p.logger.WarnContext(r.Context(), "writing egress response body", "err", err)
		}
	}
}

// resolveTargetURL determines the destination URL from the HTTP request using
// the two supported routing styles:
//
//  1. Named routing — path starts with /_egress/{route-name}/…
//  2. Transparent routing — X-Egress-URL header contains the full target URL.
func (p *Proxy) resolveTargetURL(r *http.Request) (string, error) {
	// Named routing: /_egress/{route-name}/rest/of/path
	if strings.HasPrefix(r.URL.Path, namedRoutePrefix) {
		rest := strings.TrimPrefix(r.URL.Path, namedRoutePrefix)
		// rest is "{route-name}/rest/of/path"
		slashIdx := strings.Index(rest, "/")
		var routeName, suffix string
		if slashIdx == -1 {
			routeName = rest
			suffix = ""
		} else {
			routeName = rest[:slashIdx]
			suffix = rest[slashIdx:] // includes leading slash
		}
		if routeName == "" {
			return "", fmt.Errorf("named route: route name is required in path %q", r.URL.Path)
		}
		target, err := p.resolveNamedRoute(routeName, suffix, r.URL.RawQuery)
		if err != nil {
			return "", err
		}
		return target, nil
	}

	// Transparent routing: X-Egress-URL header.
	if target := r.Header.Get(headerEgressURL); target != "" {
		return target, nil
	}

	return "", fmt.Errorf("no target URL: set %s header or use /_egress/{route-name}/path", headerEgressURL)
}

// resolveNamedRoute looks up the named route and constructs the target URL by
// replacing the route's URL scheme+host prefix with the suffix from the request
// path. If the route pattern contains glob characters, the base URL is taken as
// the longest non-glob prefix of the pattern.
func (p *Proxy) resolveNamedRoute(routeName, suffix, rawQuery string) (string, error) {
	for _, route := range p.cfg.Routes {
		if route.Name() != routeName {
			continue
		}
		base := strings.TrimRight(patternBase(route.Pattern()), "/")
		target := base + suffix
		if rawQuery != "" {
			target += "?" + rawQuery
		}
		return target, nil
	}
	return "", fmt.Errorf("unknown named route %q", routeName)
}

// patternBase returns the longest concrete prefix of a glob pattern — i.e.
// everything up to (but not including) the first glob metacharacter (*, ?, [).
// If the pattern contains no metacharacters, the full pattern is returned.
func patternBase(pattern string) string {
	for i, ch := range pattern {
		if ch == '*' || ch == '?' || ch == '[' {
			return pattern[:i]
		}
	}
	return pattern
}

// forward builds and executes an outbound HTTP request for the given egress
// request and route match, then wraps the upstream response in an EgressResponse.
//
// Header manipulation is applied in two phases:
//  1. Before forwarding: per-route injection and stripping rules are applied to
//     the outbound request headers (including always stripping X-Inject-Secret).
//     Secret injection from OpenBao is performed here when the matched route
//     carries a SecretConfig or the request carries an X-Inject-Secret header.
//  2. After receiving: per-route and default sensitive response headers are
//     stripped before the response is returned to the caller.
func (p *Proxy) forward(ctx context.Context, req domainegress.EgressRequest, match domainegress.RouteMatch) (domainegress.EgressResponse, error) {
	timeout := p.cfg.DefaultTimeout
	if match.Matched && match.Route.Timeout() > 0 {
		timeout = match.Route.Timeout()
	}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Apply per-route request header manipulation when a route was matched.
	outHeaders := req.Header
	if match.Matched {
		outHeaders = match.Route.Headers().ApplyToRequest(req.Header)
	} else {
		// Always strip X-Inject-Secret even on unmatched (allow-policy) requests.
		outHeaders = req.Header.Clone()
		outHeaders.Del(headerInjectSecret)
	}

	// Secret injection phase — must happen after header manipulation so that
	// X-Inject-Secret is extracted before being stripped.
	//
	// Two injection sources are supported (in priority order):
	//  1. Per-route static SecretConfig from the route definition.
	//  2. Dynamic X-Inject-Secret request header set by the application.
	//
	// If secret injection is required but no injector is configured, or if the
	// injector returns an error, the request is blocked (fail-closed).
	if err := p.applySecretInjection(reqCtx, req.Header, match, outHeaders); err != nil {
		p.logger.ErrorContext(ctx, "egress secret injection failed — request blocked",
			slog.String("url", req.URL),
			slog.String("err", err.Error()),
		)
		return domainegress.EgressResponse{}, fmt.Errorf("secret injection: %w", err)
	}

	body, _ := req.BodyRef.(io.Reader)
	httpReq, err := http.NewRequestWithContext(reqCtx, req.Method, req.URL, body)
	if err != nil {
		return domainegress.EgressResponse{}, fmt.Errorf("building upstream request: %w", err)
	}
	for key, vals := range outHeaders {
		for _, v := range vals {
			httpReq.Header.Add(key, v)
		}
	}

	start := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return domainegress.EgressResponse{}, fmt.Errorf("forwarding request to %s: %w", req.URL, err)
	}
	duration := time.Since(start)

	// Apply per-route response header stripping (also strips default sensitive headers).
	var respHeaders http.Header
	if match.Matched {
		respHeaders = match.Route.Headers().ApplyToResponse(resp.Header)
	} else {
		// Apply default sensitive-header stripping on unmatched requests too.
		respHeaders = domainegress.HeadersConfig{}.ApplyToResponse(resp.Header)
	}

	egressResp, err := domainegress.NewEgressResponse(resp.StatusCode, respHeaders, resp.Body, duration)
	if err != nil {
		resp.Body.Close() //nolint:errcheck
		return domainegress.EgressResponse{}, fmt.Errorf("building egress response: %w", err)
	}

	return egressResp, nil
}

// applySecretInjection resolves and injects secret values into outHeaders.
//
// It handles two injection modes:
//  1. Per-route static injection: when the matched route has a non-empty
//     SecretConfig.Name the injector fetches that secret and sets the header.
//  2. Dynamic injection: when the original request carries X-Inject-Secret,
//     that value is treated as the secret name. The secret is injected as a
//     plain value on the Authorization header unless the route provides a
//     SecretConfig that overrides the header name and format.
//
// X-Inject-Secret is always removed from outHeaders before this function
// returns, whether or not injection succeeds.
//
// Returns an error when injection is required but fails; callers must treat
// this as a hard failure and block the request.
func (p *Proxy) applySecretInjection(
	ctx context.Context,
	originalHeaders http.Header,
	match domainegress.RouteMatch,
	outHeaders http.Header,
) error {
	// Always strip X-Inject-Secret from the outbound headers — it must never
	// reach the upstream regardless of the outcome.
	dynamicSecretName := originalHeaders.Get(headerInjectSecret)
	outHeaders.Del(headerInjectSecret)

	// Determine which injection to perform.
	var cfg domainegress.SecretConfig
	if match.Matched && match.Route.Secret().Name != "" {
		// Per-route static injection takes precedence.
		cfg = match.Route.Secret()
	} else if dynamicSecretName != "" {
		// Dynamic injection: use the secret name from the request header.
		// Default to injecting as a plain Authorization header value.
		cfg = domainegress.SecretConfig{
			Name:   dynamicSecretName,
			Header: "Authorization",
			Format: "",
		}
		// If the route provides header/format overrides but no secret name,
		// apply them to the dynamic injection.
		if match.Matched {
			routeSec := match.Route.Secret()
			if routeSec.Header != "" {
				cfg.Header = routeSec.Header
			}
			if routeSec.Format != "" {
				cfg.Format = routeSec.Format
			}
		}
	} else {
		// No injection required.
		return nil
	}

	injector := p.secretInjector()
	if injector == nil {
		return fmt.Errorf("secret injection required for %q but no SecretInjector is configured", cfg.Name)
	}

	header, value, err := injector.Inject(ctx, cfg)
	if err != nil {
		return err
	}

	outHeaders.Set(header, value)
	return nil
}

// cloneAndStripHopByHop returns a copy of h with all hop-by-hop headers removed.
func cloneAndStripHopByHop(h http.Header) http.Header {
	out := h.Clone()
	for name := range hopByHopHeaders {
		out.Del(name)
	}
	// Also strip headers listed in the Connection header value.
	for _, conn := range h.Values("Connection") {
		for _, f := range strings.Split(conn, ",") {
			out.Del(strings.TrimSpace(f))
		}
	}
	return out
}

// Interface guard — Proxy must implement ports.EgressProxy.
var _ ports.EgressProxy = (*Proxy)(nil)
