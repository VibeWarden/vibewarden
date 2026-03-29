// Package egress implements the HTTP listener and request forwarding adapter
// for the egress proxy plugin. It listens on a dedicated localhost port and
// forwards outbound requests from the wrapped application to external services,
// enforcing the configured allowlist and default policy.
package egress

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// retryableStatusCodes is the set of HTTP status codes that are considered
// transient and eligible for retry.
var retryableStatusCodes = map[int]struct{}{
	http.StatusRequestTimeout:      {}, // 408
	http.StatusTooManyRequests:     {}, // 429
	http.StatusInternalServerError: {}, // 500
	http.StatusBadGateway:          {}, // 502
	http.StatusServiceUnavailable:  {}, // 503
	http.StatusGatewayTimeout:      {}, // 504
}

const (
	// namedRoutePrefix is the URL path prefix for named-route requests.
	namedRoutePrefix = "/_egress/"

	// headerEgressURL is the request header used in transparent routing mode.
	// The caller sets this header to the full target URL when POSTing to the proxy.
	headerEgressURL = "X-Egress-URL"

	// headerEgressAttempts is the response header that reports the total number
	// of upstream attempts made (initial + retries).
	headerEgressAttempts = "X-Egress-Attempts"

	// defaultListen is the default address the egress proxy binds to.
	defaultListen = "127.0.0.1:8081"

	// headerResponseTruncated is set on responses whose body was truncated
	// because it exceeded the configured response size limit.
	headerResponseTruncated = "X-Egress-Response-Truncated"

	// defaultTimeout is used when the configuration does not specify a timeout.
	defaultTimeout = 30 * time.Second

	// defaultRetryInitialBackoff is the base wait duration before the first retry
	// when RetryConfig.InitialBackoff is zero.
	defaultRetryInitialBackoff = 100 * time.Millisecond

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

	// DefaultBodySizeLimit is the global maximum allowed request body size in
	// bytes. Applied when the matched route does not set its own BodySizeLimit.
	// A value of 0 means no global limit.
	DefaultBodySizeLimit int64

	// DefaultResponseSizeLimit is the global maximum allowed response body size
	// in bytes. Applied when the matched route does not set its own
	// ResponseSizeLimit. A value of 0 means no global limit.
	DefaultResponseSizeLimit int64

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

	// CircuitBreakers, when non-nil, is the per-route circuit breaker registry
	// used to short-circuit requests to routes whose upstream has been failing.
	// When nil, circuit breaking is disabled for all routes regardless of their
	// CircuitBreakerConfig.
	CircuitBreakers *CircuitBreakerRegistry

	// RateLimiters, when non-nil, is the per-route token-bucket rate limiter
	// registry. Requests that exceed the configured rate are rejected with a
	// 429 Too Many Requests response before any upstream contact is made. When
	// nil, per-route rate limiting is disabled regardless of route configuration.
	RateLimiters *RateLimiterRegistry

	// AllowInsecure, when true, permits plain HTTP egress requests globally.
	// By default only HTTPS targets are allowed. Individual routes can also
	// override this with their AllowInsecure field.
	AllowInsecure bool

	// Metrics, when non-nil, is called to record egress request counters,
	// duration histograms, and transport-error counters. When nil, no metrics
	// are recorded.
	Metrics ports.MetricsCollector

	// EventLogger, when non-nil, is called to emit structured egress events
	// (egress.request, egress.response, egress.blocked, egress.error).
	// When nil, no events are emitted.
	EventLogger ports.EventLogger

	// Tracer, when non-nil, creates an OTel client span for each egress
	// request and propagates the W3C traceparent to the upstream.
	// When nil, no spans are created.
	Tracer ports.Tracer

	// Propagator, when non-nil, injects trace context into outbound request
	// headers (W3C traceparent). Requires Tracer to also be set.
	// When nil, no trace context is propagated.
	Propagator ports.TextMapPropagator
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
		// Enforce TLS 1.2 as minimum version on all outbound connections.
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
			transport.TLSClientConfig.MinVersion = tls.VersionTLS12
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
// request, enforces the default policy, checks the per-route circuit breaker,
// enforces body size limits, and forwards the request upstream.
func (p *Proxy) HandleRequest(ctx context.Context, req domainegress.EgressRequest) (domainegress.EgressResponse, error) {
	match, err := p.resolver.Resolve(ctx, req)
	if err != nil {
		return domainegress.EgressResponse{}, fmt.Errorf("resolving route: %w", err)
	}

	if !match.Matched {
		if p.cfg.DefaultPolicy == domainegress.PolicyDeny {
			p.emitBlocked(ctx, match, req, "no route matched default deny policy")
			return domainegress.EgressResponse{}, ErrDeniedByPolicy
		}
	}

	// Enforce TLS: reject plain HTTP targets unless explicitly permitted.
	// The effective allow_insecure flag is: per-route flag OR global flag.
	if strings.HasPrefix(req.URL, "http://") {
		routeAllows := match.Matched && match.Route.AllowInsecure()
		if !routeAllows && !p.cfg.AllowInsecure {
			p.logger.WarnContext(ctx, "egress.tls_error",
				slog.String("event_type", "egress.tls_error"),
				slog.String("url", req.URL),
				slog.String("method", req.Method),
				slog.String("reason", "plain HTTP not allowed"),
			)
			p.emitBlocked(ctx, match, req, "plain HTTP not allowed")
			return domainegress.EgressResponse{}, ErrInsecureURL
		}
	}

	// Check per-route circuit breaker before attempting the upstream call.
	if match.Matched && p.cfg.CircuitBreakers != nil {
		open, cbErr := p.cfg.CircuitBreakers.IsOpen(ctx, match.Route)
		if cbErr != nil {
			return domainegress.EgressResponse{}, fmt.Errorf("circuit breaker check: %w", cbErr)
		}
		if open {
			p.emitBlocked(ctx, match, req, "circuit breaker open")
			return domainegress.EgressResponse{}, ErrCircuitOpen
		}
	}

	// Check per-route rate limit before attempting the upstream call.
	if match.Matched && p.cfg.RateLimiters != nil {
		allowed, rlErr := p.cfg.RateLimiters.Allow(ctx, match.Route)
		if rlErr != nil {
			return domainegress.EgressResponse{}, fmt.Errorf("rate limit check: %w", rlErr)
		}
		if !allowed {
			return domainegress.EgressResponse{}, ErrRateLimitExceeded
		}
	}

	// Enforce request body size limit. The effective limit is the per-route
	// value when set, otherwise the proxy-level default.
	bodySizeLimit := p.cfg.DefaultBodySizeLimit
	if match.Matched && match.Route.BodySizeLimit() > 0 {
		bodySizeLimit = match.Route.BodySizeLimit()
	}
	if bodySizeLimit > 0 {
		limited, limitErr := p.enforceRequestBodyLimit(ctx, req, bodySizeLimit)
		if limitErr != nil {
			return domainegress.EgressResponse{}, limitErr
		}
		req = limited
	}

	resp, forwardErr := p.forward(ctx, req, match)
	if forwardErr != nil {
		// Unwrap transport errors that wrap ErrRequestBodyTooLarge — this happens
		// when the HTTP client tries to send a body that exceeds the limit.
		if errors.Is(forwardErr, ErrRequestBodyTooLarge) {
			p.logger.WarnContext(ctx, "egress.body_size_exceeded",
				slog.String("event_type", "egress.body_size_exceeded"),
				slog.String("kind", "request"),
				slog.String("url", req.URL),
				slog.String("method", req.Method),
				slog.Int64("limit_bytes", bodySizeLimit),
			)
			return domainegress.EgressResponse{}, ErrRequestBodyTooLarge
		}
		return domainegress.EgressResponse{}, forwardErr
	}
	return resp, nil
}

// emitBlocked emits an egress.blocked structured event and increments the
// egress error metric. It is called for all policy-level rejections.
func (p *Proxy) emitBlocked(ctx context.Context, match domainegress.RouteMatch, req domainegress.EgressRequest, reason string) {
	if p.cfg.EventLogger != nil {
		ev := events.NewEgressBlocked(events.EgressBlockedParams{
			Route:   routeNameOf(match),
			Method:  req.Method,
			URL:     req.URL,
			Reason:  reason,
			TraceID: traceIDFromContext(ctx),
		})
		_ = p.cfg.EventLogger.Log(ctx, ev)
	}
	if p.cfg.Metrics != nil {
		p.cfg.Metrics.IncEgressErrorTotal(routeNameOf(match))
	}
}

// enforceRequestBodyLimit checks whether the request body exceeds the limit and
// returns a (possibly modified) EgressRequest.
//
// Fast path: when Content-Length is present and already exceeds the limit,
// ErrRequestBodyTooLarge is returned immediately without reading the body.
//
// Slow path: the body is wrapped with a limitedBody reader that returns
// ErrRequestBodyTooLarge if more than limit bytes are read.
func (p *Proxy) enforceRequestBodyLimit(ctx context.Context, req domainegress.EgressRequest, limit int64) (domainegress.EgressRequest, error) {
	// Fast path: check Content-Length header first to avoid reading the body.
	if cl := req.Header.Get("Content-Length"); cl != "" {
		var n int64
		if _, err := fmt.Sscanf(cl, "%d", &n); err == nil && n > limit {
			p.logger.WarnContext(ctx, "egress.body_size_exceeded",
				slog.String("event_type", "egress.body_size_exceeded"),
				slog.String("kind", "request"),
				slog.String("url", req.URL),
				slog.String("method", req.Method),
				slog.Int64("limit_bytes", limit),
				slog.Int64("content_length", n),
			)
			return req, ErrRequestBodyTooLarge
		}
	}

	// Slow path: wrap the body so over-limit reads surface as ErrRequestBodyTooLarge.
	if body, ok := req.BodyRef.(io.Reader); ok && body != nil {
		req.BodyRef = &limitedBody{
			r:     io.LimitReader(body, limit+1),
			limit: limit,
		}
	}
	return req, nil
}

// limitedBody is an io.ReadCloser that wraps an io.LimitReader and returns
// ErrRequestBodyTooLarge when the caller attempts to read more than limit bytes.
type limitedBody struct {
	r     io.Reader
	limit int64
	read  int64
}

// Read implements io.Reader. It returns ErrRequestBodyTooLarge when more than
// limit bytes have been read from the underlying stream.
func (lb *limitedBody) Read(p []byte) (int, error) {
	n, err := lb.r.Read(p)
	lb.read += int64(n)
	if lb.read > lb.limit {
		return n, ErrRequestBodyTooLarge
	}
	return n, err
}

// Close implements io.Closer. It is a no-op because the underlying LimitReader
// does not implement io.Closer.
func (lb *limitedBody) Close() error { return nil }

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
		if err == ErrInsecureURL {
			p.logger.WarnContext(r.Context(), "egress.tls_error",
				slog.String("event_type", "egress.tls_error"),
				slog.String("target", targetURL),
				slog.String("method", r.Method),
				slog.String("reason", "plain HTTP not allowed"),
			)
			http.Error(w, "400 Bad Request: "+ErrInsecureURL.Error(), http.StatusBadRequest)
			return
		}
		if err == ErrRequestBodyTooLarge {
			p.logger.WarnContext(r.Context(), "egress.body_size_exceeded",
				slog.String("event_type", "egress.body_size_exceeded"),
				slog.String("kind", "request"),
				slog.String("target", targetURL),
				slog.String("method", r.Method),
			)
			http.Error(w, "413 Request Entity Too Large: request body exceeds egress size limit", http.StatusRequestEntityTooLarge)
			return
		}
		if err == ErrCircuitOpen {
			p.logger.WarnContext(r.Context(), "egress circuit breaker open — request rejected",
				slog.String("target", targetURL),
				slog.String("method", r.Method),
			)
			http.Error(w, "503 Service Unavailable: egress circuit breaker is open", http.StatusServiceUnavailable)
			return
		}
		if err == ErrRateLimitExceeded {
			// Resolve the matched route to compute Retry-After.
			retryAfter := "1"
			if p.cfg.RateLimiters != nil {
				egressReq2, reqErr := domainegress.NewEgressRequest(r.Method, targetURL, nil, nil)
				if reqErr == nil {
					if m2, resolveErr := p.resolver.Resolve(r.Context(), egressReq2); resolveErr == nil && m2.Matched {
						if secs, raErr := p.cfg.RateLimiters.RetryAfterSeconds(m2.Route); raErr == nil {
							retryAfter = retryAfterHeader(secs)
						}
					}
				}
			}
			p.logger.WarnContext(r.Context(), "egress rate limit exceeded — request rejected",
				slog.String("target", targetURL),
				slog.String("method", r.Method),
				slog.String("retry_after", retryAfter),
			)
			w.Header().Set("Retry-After", retryAfter)
			http.Error(w, "429 Too Many Requests: egress rate limit exceeded", http.StatusTooManyRequests)
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
		// A deadline-exceeded error from context.WithTimeout in forward() means
		// the upstream did not respond within the configured timeout.
		if isTimeoutError(err) {
			p.logger.WarnContext(r.Context(), "egress request timed out",
				slog.String("target", targetURL),
				slog.String("method", r.Method),
			)
			http.Error(w, "504 Gateway Timeout: upstream did not respond in time", http.StatusGatewayTimeout)
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

	// Determine the effective response size limit before sending headers so we
	// can declare the truncation trailer key in advance.
	respSizeLimit := p.responseSizeLimitFor(egressReq)

	respHeaders := cloneAndStripHopByHop(egressResp.Header)
	// When a response size limit is active we cannot forward Content-Length
	// because the actual bytes written may be fewer than the upstream reported.
	// Removing it forces chunked transfer encoding, which is required for
	// HTTP/1.1 trailers to work correctly.
	if respSizeLimit > 0 {
		respHeaders.Del("Content-Length")
	}
	for key, vals := range respHeaders {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	// Report total attempt count (initial + retries) to the caller.
	if egressResp.Attempts > 0 {
		w.Header().Set(headerEgressAttempts, fmt.Sprintf("%d", egressResp.Attempts))
	}
	// Announce the truncation trailer so HTTP/1.1 clients can read it after the body.
	if respSizeLimit > 0 {
		w.Header().Add("Trailer", headerResponseTruncated)
	}
	w.WriteHeader(egressResp.StatusCode)

	if respBody != nil {
		defer respBody.Close() //nolint:errcheck

		if respSizeLimit > 0 {
			// Copy at most respSizeLimit bytes.
			written, copyErr := io.Copy(w, io.LimitReader(respBody, respSizeLimit))
			// Try to read one more byte to detect whether the body was truncated.
			var probe [1]byte
			n, _ := respBody.Read(probe[:])
			if n > 0 {
				// Body exceeded the limit — log and set the truncation trailer.
				p.logger.WarnContext(r.Context(), "egress.body_size_exceeded",
					slog.String("event_type", "egress.body_size_exceeded"),
					slog.String("kind", "response"),
					slog.String("target", targetURL),
					slog.String("method", r.Method),
					slog.Int64("limit_bytes", respSizeLimit),
					slog.Int64("bytes_written", written),
				)
				w.Header().Set(headerResponseTruncated, "true")
			}
			if copyErr != nil {
				p.logger.WarnContext(r.Context(), "writing egress response body", "err", copyErr)
			}
		} else {
			if _, err := io.Copy(w, respBody); err != nil {
				p.logger.WarnContext(r.Context(), "writing egress response body", "err", err)
			}
		}
	}
}

// responseSizeLimitFor returns the effective response size limit for the given
// egress request. The per-route limit takes precedence over the proxy default.
func (p *Proxy) responseSizeLimitFor(req domainegress.EgressRequest) int64 {
	// Re-resolve the route to get per-route settings. We use a background
	// context because this is a cheap in-memory lookup.
	match, err := p.resolver.Resolve(context.Background(), req)
	if err == nil && match.Matched && match.Route.ResponseSizeLimit() > 0 {
		return match.Route.ResponseSizeLimit()
	}
	return p.cfg.DefaultResponseSizeLimit
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
//
// When the matched route has a RetryConfig, transient failures on idempotent
// methods are retried with exponential or fixed backoff. Each retry attempt is
// logged as an egress.retry structured event. A timeout from context.WithTimeout
// is returned as-is so the HTTP handler can respond with 504.
func (p *Proxy) forward(ctx context.Context, req domainegress.EgressRequest, match domainegress.RouteMatch) (domainegress.EgressResponse, error) {
	// --- Observability: start client span ---
	routeName := routeNameOf(match)
	spanCtx := ctx
	var span ports.Span
	if p.cfg.Tracer != nil {
		spanCtx, span = p.cfg.Tracer.Start(ctx, "egress "+req.Method,
			ports.WithSpanKind(ports.SpanKindClient))
		// Store trace-id in context for structured log correlation.
		// We extract it from the span context via a no-op propagation trick:
		// instead, store the routeName and let the span carry the trace.
		defer span.End()
		span.SetAttributes(
			ports.Attribute{Key: "http.request.method", Value: req.Method},
			ports.Attribute{Key: "url.full", Value: req.URL},
			ports.Attribute{Key: "egress.route", Value: routeName},
		)
	}

	// Emit egress.request event.
	if p.cfg.EventLogger != nil {
		_ = p.cfg.EventLogger.Log(spanCtx, events.NewEgressRequest(events.EgressRequestParams{
			Route:   routeName,
			Method:  req.Method,
			URL:     req.URL,
			TraceID: traceIDFromContext(spanCtx),
		}))
	}

	timeout := p.cfg.DefaultTimeout
	if match.Matched && match.Route.Timeout() > 0 {
		timeout = match.Route.Timeout()
	}

	reqCtx, cancel := context.WithTimeout(spanCtx, timeout)
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

	// Determine retry parameters. Retry is only attempted for matched routes
	// that carry a RetryConfig with Max > 0 and an idempotent method.
	retryCfg := domainegress.RetryConfig{}
	retryEnabled := false
	if match.Matched {
		rc := match.Route.Retry()
		if rc.Max > 0 && rc.IsRetryableMethod(req.Method) {
			retryCfg = rc
			retryEnabled = true
		}
	}

	maxAttempts := 1
	if retryEnabled {
		maxAttempts = 1 + retryCfg.Max
	}

	initialBackoff := retryCfg.InitialBackoff
	if initialBackoff <= 0 {
		initialBackoff = defaultRetryInitialBackoff
	}

	var (
		lastResp     *http.Response
		lastErr      error
		start        = time.Now()
		attemptsDone int
	)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptsDone = attempt

		// Each attempt needs a fresh HTTP request because the body and context
		// cannot be reused after the first send.
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

		// Inject W3C traceparent into outbound request headers so the external
		// service can continue the trace.
		if p.cfg.Propagator != nil {
			p.cfg.Propagator.Inject(reqCtx, httpHeaderCarrier(httpReq.Header))
		}

		resp, err := p.client.Do(httpReq)

		if err != nil {
			lastErr = err
			lastResp = nil

			// A context deadline/cancellation is a timeout — do not retry, surface
			// it immediately so the caller can return 504.
			if isTimeoutError(err) {
				p.logger.WarnContext(ctx, "egress.timeout",
					slog.String("event_type", "egress.timeout"),
					slog.String("url", req.URL),
					slog.String("method", req.Method),
					slog.Int("attempt", attempt),
				)
				if match.Matched && p.cfg.CircuitBreakers != nil {
					p.cfg.CircuitBreakers.RecordFailure(ctx, match.Route)
				}
				wrappedErr := fmt.Errorf("forwarding request to %s: %w", req.URL, err)
				p.recordEgressError(spanCtx, span, match, req, routeName, attempt, time.Since(start), wrappedErr)
				return domainegress.EgressResponse{}, wrappedErr
			}

			if retryEnabled && attempt < maxAttempts {
				backoff := computeBackoff(retryCfg.Backoff, initialBackoff, attempt)
				p.logger.WarnContext(ctx, "egress.retry",
					slog.String("event_type", "egress.retry"),
					slog.String("url", req.URL),
					slog.String("method", req.Method),
					slog.Int("attempt", attempt),
					slog.Int("max_attempts", maxAttempts),
					slog.String("backoff", backoff.String()),
					slog.String("reason", err.Error()),
				)
				if !sleep(reqCtx, backoff) {
					// Context cancelled during backoff — surface timeout.
					if match.Matched && p.cfg.CircuitBreakers != nil {
						p.cfg.CircuitBreakers.RecordFailure(ctx, match.Route)
					}
					wrappedErr := fmt.Errorf("forwarding request to %s: %w", req.URL, reqCtx.Err())
					p.recordEgressError(spanCtx, span, match, req, routeName, attempt, time.Since(start), wrappedErr)
					return domainegress.EgressResponse{}, wrappedErr
				}
				continue
			}

			if match.Matched && p.cfg.CircuitBreakers != nil {
				p.cfg.CircuitBreakers.RecordFailure(ctx, match.Route)
			}
			// lastErr is set — break the loop so the post-loop check records
			// observability and returns the error.
			break
		}

		// We have a response. Check whether it is a retryable status code.
		if retryEnabled && attempt < maxAttempts {
			if _, retryable := retryableStatusCodes[resp.StatusCode]; retryable {
				// Drain and close the body before retrying to free the connection.
				resp.Body.Close() //nolint:errcheck
				backoff := computeBackoff(retryCfg.Backoff, initialBackoff, attempt)
				p.logger.WarnContext(ctx, "egress.retry",
					slog.String("event_type", "egress.retry"),
					slog.String("url", req.URL),
					slog.String("method", req.Method),
					slog.Int("attempt", attempt),
					slog.Int("max_attempts", maxAttempts),
					slog.String("backoff", backoff.String()),
					slog.String("reason", fmt.Sprintf("status %d", resp.StatusCode)),
				)
				if !sleep(reqCtx, backoff) {
					wrappedErr := fmt.Errorf("forwarding request to %s: %w", req.URL, reqCtx.Err())
					p.recordEgressError(spanCtx, span, match, req, routeName, attempt, time.Since(start), wrappedErr)
					return domainegress.EgressResponse{}, wrappedErr
				}
				continue
			}
		}

		lastResp = resp
		lastErr = nil
		break
	}

	if lastErr != nil {
		duration := time.Since(start)
		p.recordEgressError(spanCtx, span, match, req, routeName, attemptsDone, duration, lastErr)
		return domainegress.EgressResponse{}, fmt.Errorf("forwarding request to %s: %w", req.URL, lastErr)
	}

	// Record circuit breaker outcome based on the final response status.
	if match.Matched && p.cfg.CircuitBreakers != nil {
		if isFailureStatus(lastResp.StatusCode) {
			p.cfg.CircuitBreakers.RecordFailure(ctx, match.Route)
		} else {
			p.cfg.CircuitBreakers.RecordSuccess(ctx, match.Route)
		}
	}

	duration := time.Since(start)

	// --- Observability: record successful response metrics and events ---
	if p.cfg.Metrics != nil {
		p.cfg.Metrics.IncEgressRequestTotal(routeName, req.Method, strconv.Itoa(lastResp.StatusCode))
		p.cfg.Metrics.ObserveEgressDuration(routeName, req.Method, duration)
	}
	if p.cfg.EventLogger != nil {
		_ = p.cfg.EventLogger.Log(spanCtx, events.NewEgressResponse(events.EgressResponseParams{
			Route:           routeName,
			Method:          req.Method,
			URL:             req.URL,
			StatusCode:      lastResp.StatusCode,
			DurationSeconds: duration.Seconds(),
			Attempts:        attemptsDone,
			TraceID:         traceIDFromContext(spanCtx),
		}))
	}
	if span != nil {
		span.SetAttributes(
			ports.Attribute{Key: "http.response.status_code", Value: strconv.Itoa(lastResp.StatusCode)},
		)
		if lastResp.StatusCode >= 500 {
			span.SetStatus(ports.SpanStatusError, http.StatusText(lastResp.StatusCode))
		} else {
			span.SetStatus(ports.SpanStatusOK, "")
		}
	}

	// Apply per-route response header stripping (also strips default sensitive headers).
	var respHeaders http.Header
	if match.Matched {
		respHeaders = match.Route.Headers().ApplyToResponse(lastResp.Header)
	} else {
		// Apply default sensitive-header stripping on unmatched requests too.
		respHeaders = domainegress.HeadersConfig{}.ApplyToResponse(lastResp.Header)
	}

	egressResp, err := domainegress.NewEgressResponse(lastResp.StatusCode, respHeaders, lastResp.Body, duration)
	if err != nil {
		lastResp.Body.Close() //nolint:errcheck
		return domainegress.EgressResponse{}, fmt.Errorf("building egress response: %w", err)
	}

	// Record the total number of upstream attempts so the HTTP handler can set
	// the X-Egress-Attempts response header.
	egressResp.Attempts = attemptsDone
	return egressResp, nil
}

// recordEgressError records observability for a transport-level egress failure.
// It emits an egress.error structured event, increments the error and request
// counters, records the duration histogram, and marks the span as errored.
func (p *Proxy) recordEgressError(
	ctx context.Context,
	span ports.Span,
	match domainegress.RouteMatch,
	req domainegress.EgressRequest,
	routeName string,
	attempts int,
	duration time.Duration,
	lastErr error,
) {
	if p.cfg.Metrics != nil {
		p.cfg.Metrics.IncEgressRequestTotal(routeName, req.Method, "error")
		p.cfg.Metrics.ObserveEgressDuration(routeName, req.Method, duration)
		p.cfg.Metrics.IncEgressErrorTotal(routeName)
	}
	if p.cfg.EventLogger != nil {
		_ = p.cfg.EventLogger.Log(ctx, events.NewEgressError(events.EgressErrorParams{
			Route:    routeName,
			Method:   req.Method,
			URL:      req.URL,
			Error:    lastErr.Error(),
			Attempts: attempts,
			TraceID:  traceIDFromContext(ctx),
		}))
	}
	if span != nil {
		span.RecordError(lastErr)
		span.SetStatus(ports.SpanStatusError, lastErr.Error())
	}
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

// computeBackoff returns the wait duration before attempt number n (1-based)
// using the given strategy. For exponential backoff the base is doubled on each
// attempt: base * 2^(n-1). For fixed backoff the same base is always returned.
func computeBackoff(strategy domainegress.RetryBackoff, base time.Duration, attempt int) time.Duration {
	if strategy == domainegress.RetryBackoffFixed {
		return base
	}
	// Default: exponential. Shift by (attempt-1) doublings.
	shift := attempt - 1
	if shift > 30 {
		shift = 30 // guard against overflow on absurdly high Max values
	}
	return base * (1 << uint(shift))
}

// sleep blocks for d using a timer that respects context cancellation.
// Returns true if the sleep completed, false if the context was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// isTimeoutError reports whether err represents a context deadline exceeded or
// context cancellation originating from the per-request timeout. It also covers
// net/http timeout errors that wrap url.Error with a Timeout() method.
func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	// net/http wraps transport errors in *url.Error; check the Timeout() method.
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// isFailureStatus reports whether the given HTTP status code should be treated
// as an upstream failure for circuit breaker purposes. 5xx server errors are
// considered failures; client errors (4xx) and successful responses are not.
func isFailureStatus(code int) bool {
	return code >= http.StatusInternalServerError
}

// routeNameOf returns the matched route name or "unmatched" when no route matched.
// It is used to populate low-cardinality metric and event labels.
func routeNameOf(match domainegress.RouteMatch) string {
	if match.Matched {
		return match.Route.Name()
	}
	return "unmatched"
}

// traceIDFromContext extracts the W3C trace-id string from ctx as a hex string.
// Returns an empty string when no span is active in ctx or tracing is disabled.
func traceIDFromContext(ctx context.Context) string {
	type traceIDer interface {
		TraceID() [16]byte
		IsValid() bool
	}
	// Use OTel SDK directly would create a hard dependency on the SDK here.
	// Instead we store the trace-id in the context via a key when we start a span.
	if id, ok := ctx.Value(ctxKeyTraceID{}).(string); ok {
		return id
	}
	return ""
}

// ctxKeyTraceID is the context key used to store the egress span trace-id.
type ctxKeyTraceID struct{}

// httpHeaderCarrier adapts http.Header to ports.TextMapCarrier for W3C trace
// context propagation on outbound egress requests.
type httpHeaderCarrier http.Header

func (c httpHeaderCarrier) Get(key string) string { return http.Header(c).Get(key) }
func (c httpHeaderCarrier) Set(key, value string) { http.Header(c).Set(key, value) }
func (c httpHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Interface guard — Proxy must implement ports.EgressProxy.
var _ ports.EgressProxy = (*Proxy)(nil)
