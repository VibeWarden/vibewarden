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
	"strings"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/middleware"
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

	// ResponseCaches, when non-nil, is the per-route in-memory response cache
	// registry. GET and HEAD requests that match a route with cache.enabled=true
	// will be served from the cache when a valid entry exists, and the upstream
	// response will be stored in the cache on a 2xx status. When nil, response
	// caching is disabled for all routes regardless of route configuration.
	ResponseCaches *ResponseCacheRegistry

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

	// MTLSClients is an optional per-route map of *http.Client instances that
	// carry route-specific mTLS client certificates. When a matched route has
	// an entry in this map, its dedicated client is used for that request
	// instead of the proxy default client. Build this map with BuildMTLSClients.
	MTLSClients MTLSClientMap

	// PromptInjectionRoutes, when non-empty, enables prompt injection scanning
	// for matching egress routes. The middleware runs before the request is
	// forwarded to the upstream. Build this slice with
	// middleware.BuildPromptInjectionRoutes in the plugin init path.
	PromptInjectionRoutes []middleware.PromptInjectionRouteConfig
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

	// Wrap the mux with the prompt injection middleware when routes are configured.
	// The middleware runs before handleRequest so that injected requests are
	// blocked prior to any upstream contact.
	var handler http.Handler = mux
	if len(p.cfg.PromptInjectionRoutes) > 0 {
		handler = middleware.PromptInjectionMiddleware(p.cfg.PromptInjectionRoutes, p.logger, p.cfg.EventLogger, nil)(mux)
	}

	p.server = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := p.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
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

	// Check in-memory response cache for cacheable requests.
	// Only GET and HEAD requests on routes with cache.enabled=true are eligible.
	if match.Matched && p.cfg.ResponseCaches != nil {
		cache := p.cfg.ResponseCaches.CacheFor(match.Route)
		if cache != nil {
			if entry, hit := cache.Get(req.Method, req.URL, time.Now()); hit {
				return egressResponseFromCache(entry, 0), nil
			}
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

	// Validate the upstream response against per-route rules when configured.
	if match.Matched {
		if err := p.validateResponse(ctx, req, match, resp); err != nil {
			// Close the upstream body — we will not forward it.
			if rc, ok := resp.BodyRef.(io.ReadCloser); ok && rc != nil {
				rc.Close() //nolint:errcheck
			}
			return domainegress.EgressResponse{}, err
		}
	}

	// Store a cacheable response in the route's in-memory cache.
	// The body is fully read and buffered; the response returned to the caller
	// gets a fresh reader over the same bytes.
	if match.Matched && p.cfg.ResponseCaches != nil && isCacheable(req.Method, resp.StatusCode) {
		cache := p.cfg.ResponseCaches.CacheFor(match.Route)
		if cache != nil {
			key := cacheKey{method: req.Method, url: req.URL}
			entry, freshBody, readErr := cacheableResponse(resp, match.Route.Cache(), key, time.Now())
			if readErr == nil {
				resp.BodyRef = freshBody
				// Set MISS header to indicate this response came from upstream.
				resp.Header.Set(headerEgressCache, "MISS")
				if entry != nil {
					cache.Set(entry)
				}
			}
		}
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

// validateResponse checks the upstream response against the per-route
// validate_response rules. It returns ErrResponseValidationFailed (with context)
// when the status code or content type is not in the configured allowlists, and
// emits an egress.response_invalid structured event. It returns nil when the
// route has no validation config or the response passes all checks.
func (p *Proxy) validateResponse(ctx context.Context, req domainegress.EgressRequest, match domainegress.RouteMatch, resp domainegress.EgressResponse) error {
	valCfg := match.Route.ValidateResponse()
	if valCfg.IsZero() {
		return nil
	}

	routeName := routeNameOf(match)

	// Check status code allowlist.
	if !valCfg.MatchesStatusCode(resp.StatusCode) {
		reason := fmt.Sprintf("status code %d not in allowed list", resp.StatusCode)
		ct := resp.Header.Get("Content-Type")
		p.logger.WarnContext(ctx, "egress.response_invalid",
			slog.String("event_type", "egress.response_invalid"),
			slog.String("route", routeName),
			slog.String("url", req.URL),
			slog.String("method", req.Method),
			slog.Int("status_code", resp.StatusCode),
			slog.String("content_type", ct),
			slog.String("reason", reason),
		)
		p.emitResponseInvalid(ctx, req, routeName, resp.StatusCode, ct, reason)
		return fmt.Errorf("%w: %s", ErrResponseValidationFailed, reason)
	}

	// Check content type allowlist.
	ct := resp.Header.Get("Content-Type")
	if !valCfg.MatchesContentType(ct) {
		reason := fmt.Sprintf("content type %q not in allowed list", ct)
		p.logger.WarnContext(ctx, "egress.response_invalid",
			slog.String("event_type", "egress.response_invalid"),
			slog.String("route", routeName),
			slog.String("url", req.URL),
			slog.String("method", req.Method),
			slog.Int("status_code", resp.StatusCode),
			slog.String("content_type", ct),
			slog.String("reason", reason),
		)
		p.emitResponseInvalid(ctx, req, routeName, resp.StatusCode, ct, reason)
		return fmt.Errorf("%w: %s", ErrResponseValidationFailed, reason)
	}

	return nil
}

// emitResponseInvalid emits an egress.response_invalid structured event and
// increments the egress error metric.
func (p *Proxy) emitResponseInvalid(ctx context.Context, req domainegress.EgressRequest, routeName string, statusCode int, contentType, reason string) {
	if p.cfg.EventLogger != nil {
		ev := events.NewEgressResponseInvalid(events.EgressResponseInvalidParams{
			Route:       routeName,
			Method:      req.Method,
			URL:         req.URL,
			StatusCode:  statusCode,
			ContentType: contentType,
			Reason:      reason,
			TraceID:     traceIDFromContext(ctx),
		})
		_ = p.cfg.EventLogger.Log(ctx, ev)
	}
	if p.cfg.Metrics != nil {
		p.cfg.Metrics.IncEgressErrorTotal(routeName)
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
	egressReq, targetURL, err := p.parseIncomingRequest(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("bad request: %s", err), http.StatusBadRequest)
		return
	}

	egressResp, err := p.HandleRequest(r.Context(), egressReq)
	if err != nil {
		p.writeEgressErrorResponse(w, r, targetURL, err)
		return
	}

	p.writeEgressResponse(w, r, egressReq, egressResp, targetURL)
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
	routeName := routeNameOf(match)
	spanCtx, span := p.startEgressSpan(ctx, req, routeName)
	defer endSpan(span)

	p.emitEgressRequest(spanCtx, req, routeName)

	reqCtx, cancel := p.attemptContext(spanCtx, match)
	defer cancel()

	var err error
	var outHeaders http.Header
	req, outHeaders, err = p.prepareOutboundRequest(reqCtx, ctx, req, match, routeName)
	if err != nil {
		return domainegress.EgressResponse{}, err
	}

	lastResp, attempts, start, err := p.executeWithRetries(reqCtx, ctx, spanCtx, span, req, outHeaders, match, routeName)
	if err != nil {
		return domainegress.EgressResponse{}, err
	}

	p.recordCircuitOutcome(ctx, match, lastResp)
	duration := time.Since(start)
	p.recordEgressSuccess(spanCtx, span, match, req, routeName, attempts, duration, lastResp)

	return p.buildEgressResponse(lastResp, match, attempts, duration)
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
// The trace-id is stored in the context via a key when a span is started, avoiding
// a hard dependency on the OTel SDK in this package.
func traceIDFromContext(ctx context.Context) string {
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
