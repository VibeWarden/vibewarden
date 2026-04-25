package egress

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// startEgressSpan starts an OTel client span for the egress request and returns
// the enriched context and the span. When Tracer is nil both return values are
// the original ctx and nil respectively.
func (p *Proxy) startEgressSpan(ctx context.Context, req domainegress.EgressRequest, routeName string) (context.Context, ports.Span) {
	if p.cfg.Tracer == nil {
		return ctx, nil
	}
	spanCtx, span := p.cfg.Tracer.Start(ctx, "egress "+req.Method,
		ports.WithSpanKind(ports.SpanKindClient))
	span.SetAttributes(
		ports.Attribute{Key: "http.request.method", Value: req.Method},
		ports.Attribute{Key: "url.full", Value: req.URL},
		ports.Attribute{Key: "egress.route", Value: routeName},
	)
	return spanCtx, span
}

// endSpan is a nil-safe defer helper that ends the span.
func endSpan(span ports.Span) {
	if span != nil {
		span.End()
	}
}

// emitEgressRequest emits an egress.request structured event.
func (p *Proxy) emitEgressRequest(ctx context.Context, req domainegress.EgressRequest, routeName string) {
	if p.cfg.EventLogger == nil {
		return
	}
	_ = p.cfg.EventLogger.Log(ctx, events.NewEgressRequest(events.EgressRequestParams{
		Route:   routeName,
		Method:  req.Method,
		URL:     req.URL,
		TraceID: traceIDFromContext(ctx),
	}))
}

// attemptContext creates a per-request context with the effective timeout.
// The effective timeout is the per-route timeout when set, otherwise the proxy default.
func (p *Proxy) attemptContext(ctx context.Context, match domainegress.RouteMatch) (context.Context, context.CancelFunc) {
	timeout := p.cfg.DefaultTimeout
	if match.Matched && match.Route.Timeout() > 0 {
		timeout = match.Route.Timeout()
	}
	return context.WithTimeout(ctx, timeout)
}

// applyHeaderManipulation applies per-route request header rules to req.Header
// and returns the outbound header map. X-Inject-Secret is always stripped even
// on unmatched (allow-policy) requests.
func applyHeaderManipulation(req domainegress.EgressRequest, match domainegress.RouteMatch) http.Header {
	if match.Matched {
		return match.Route.Headers().ApplyToRequest(req.Header)
	}
	out := req.Header.Clone()
	out.Del(headerInjectSecret)
	return out
}

// prepareOutboundRequest applies header manipulation, PII sanitization, and
// secret injection to the request. It returns the (possibly modified) request
// and the outbound header map to use for the upstream call.
// ctx is the request-scoped context (with timeout); logCtx is the span context
// used for structured log correlation.
func (p *Proxy) prepareOutboundRequest(
	ctx context.Context,
	logCtx context.Context,
	req domainegress.EgressRequest,
	match domainegress.RouteMatch,
	routeName string,
) (domainegress.EgressRequest, http.Header, error) {
	outHeaders := applyHeaderManipulation(req, match)

	if match.Matched {
		sanitizeCfg := match.Route.Sanitize()
		if !sanitizeCfg.IsZero() {
			sanitizedReq, _, sanitizeResult, sanitizeErr := sanitizeRequest(ctx, req, sanitizeCfg)
			if sanitizeErr != nil {
				p.logger.ErrorContext(logCtx, "egress sanitization error — request blocked",
					slog.String("url", req.URL),
					slog.String("err", sanitizeErr.Error()),
				)
				return req, nil, fmt.Errorf("sanitizing request: %w", sanitizeErr)
			}
			req = sanitizedReq
			p.emitSanitized(ctx, routeName, req, sanitizeResult)
		}
	}

	if err := p.applySecretInjection(ctx, req.Header, match, outHeaders); err != nil {
		p.logger.ErrorContext(logCtx, "egress secret injection failed — request blocked",
			slog.String("url", req.URL),
			slog.String("err", err.Error()),
		)
		return req, nil, fmt.Errorf("secret injection: %w", err)
	}

	return req, outHeaders, nil
}

// buildUpstreamRequest creates a fresh *http.Request for a single upstream attempt.
// It copies outHeaders and injects W3C trace context when a Propagator is configured.
func (p *Proxy) buildUpstreamRequest(ctx context.Context, req domainegress.EgressRequest, outHeaders http.Header) (*http.Request, error) {
	body, _ := req.BodyRef.(io.Reader)
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return nil, fmt.Errorf("building upstream request: %w", err)
	}
	for key, vals := range outHeaders {
		for _, v := range vals {
			httpReq.Header.Add(key, v)
		}
	}
	if p.cfg.Propagator != nil {
		p.cfg.Propagator.Inject(ctx, httpHeaderCarrier(httpReq.Header))
	}
	return httpReq, nil
}

// selectHTTPClient returns the per-route mTLS client when one is configured for
// the matched route, otherwise falls back to the proxy default client.
func (p *Proxy) selectHTTPClient(match domainegress.RouteMatch) *http.Client {
	if match.Matched {
		if mtlsClient, ok := p.cfg.MTLSClients[match.Route.Name()]; ok {
			return mtlsClient
		}
	}
	return p.client
}

// shouldRetryStatus reports whether the given HTTP status code is eligible for
// retry. Only the codes listed in retryableStatusCodes are considered transient.
func shouldRetryStatus(code int) bool {
	_, ok := retryableStatusCodes[code]
	return ok
}

// classifyTransportError inspects a transport-level error and decides how to
// handle it. It returns (fatal=true, wrappedErr) for errors that must not be
// retried (mTLS handshake failure, context timeout/cancellation), and
// (fatal=false, nil) when the attempt is eligible for retry.
// ctx is the parent/log context; spanCtx carries the OTel span.
// In all fatal cases the appropriate observability is recorded.
func (p *Proxy) classifyTransportError(
	ctx context.Context,
	spanCtx context.Context,
	span ports.Span,
	match domainegress.RouteMatch,
	req domainegress.EgressRequest,
	routeName string,
	attempt int,
	start time.Time,
	err error,
) (fatal bool, wrapped error) {
	// mTLS handshake failure — non-retryable.
	if match.Matched && !match.Route.MTLS().IsZero() && isMTLSError(err) {
		p.logger.ErrorContext(ctx, "egress.mtls_error",
			slog.String("event_type", "egress.mtls_error"),
			slog.String("url", req.URL),
			slog.String("method", req.Method),
			slog.String("route", routeName),
			slog.String("err", err.Error()),
		)
		wrappedErr := fmt.Errorf("%w: %w", ErrMTLSHandshakeFailed, err)
		p.recordEgressError(spanCtx, span, match, req, routeName, attempt, time.Since(start), wrappedErr)
		return true, wrappedErr
	}

	// Timeout — non-retryable.
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
		return true, wrappedErr
	}

	return false, nil
}

// executeWithRetries runs the upstream call with retry logic. It returns the
// last successful *http.Response, the number of attempts made, the start time,
// and any terminal error. Retries are only attempted for idempotent methods on
// matched routes with a RetryConfig.Max > 0.
// ctx is the request-scoped context (with timeout); parentCtx is the caller
// context used for circuit breaker and log correlation; spanCtx carries OTel span.
func (p *Proxy) executeWithRetries(
	ctx context.Context,
	parentCtx context.Context,
	spanCtx context.Context,
	span ports.Span,
	req domainegress.EgressRequest,
	outHeaders http.Header,
	match domainegress.RouteMatch,
	routeName string,
) (*http.Response, int, time.Time, error) {
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

		httpReq, err := p.buildUpstreamRequest(ctx, req, outHeaders)
		if err != nil {
			return nil, attemptsDone, start, err
		}

		activeClient := p.selectHTTPClient(match)
		resp, err := activeClient.Do(httpReq) //nolint:gosec // G704: SSRF is guarded by SSRFGuard on the transport layer

		if err != nil {
			lastErr = err
			lastResp = nil

			fatal, wrappedErr := p.classifyTransportError(parentCtx, spanCtx, span, match, req, routeName, attempt, start, err)
			if fatal {
				return nil, attemptsDone, start, wrappedErr
			}

			if retryEnabled && attempt < maxAttempts {
				backoff := computeBackoff(retryCfg.Backoff, initialBackoff, attempt)
				p.logger.WarnContext(parentCtx, "egress.retry",
					slog.String("event_type", "egress.retry"),
					slog.String("url", req.URL),
					slog.String("method", req.Method),
					slog.Int("attempt", attempt),
					slog.Int("max_attempts", maxAttempts),
					slog.String("backoff", backoff.String()),
					slog.String("reason", err.Error()),
				)
				if !sleep(ctx, backoff) {
					if match.Matched && p.cfg.CircuitBreakers != nil {
						p.cfg.CircuitBreakers.RecordFailure(parentCtx, match.Route)
					}
					wrappedErr := fmt.Errorf("forwarding request to %s: %w", req.URL, ctx.Err())
					p.recordEgressError(spanCtx, span, match, req, routeName, attempt, time.Since(start), wrappedErr)
					return nil, attemptsDone, start, wrappedErr
				}
				continue
			}

			if match.Matched && p.cfg.CircuitBreakers != nil {
				p.cfg.CircuitBreakers.RecordFailure(parentCtx, match.Route)
			}
			break
		}

		// We have a response. Check whether it is a retryable status code.
		if retryEnabled && attempt < maxAttempts && shouldRetryStatus(resp.StatusCode) {
			resp.Body.Close() //nolint:errcheck
			backoff := computeBackoff(retryCfg.Backoff, initialBackoff, attempt)
			p.logger.WarnContext(parentCtx, "egress.retry",
				slog.String("event_type", "egress.retry"),
				slog.String("url", req.URL),
				slog.String("method", req.Method),
				slog.Int("attempt", attempt),
				slog.Int("max_attempts", maxAttempts),
				slog.String("backoff", backoff.String()),
				slog.String("reason", fmt.Sprintf("status %d", resp.StatusCode)),
			)
			if !sleep(ctx, backoff) {
				wrappedErr := fmt.Errorf("forwarding request to %s: %w", req.URL, ctx.Err())
				p.recordEgressError(spanCtx, span, match, req, routeName, attempt, time.Since(start), wrappedErr)
				return nil, attemptsDone, start, wrappedErr
			}
			continue
		}

		lastResp = resp
		lastErr = nil
		break
	}

	if lastErr != nil {
		duration := time.Since(start)
		p.recordEgressError(spanCtx, span, match, req, routeName, attemptsDone, duration, lastErr)
		return nil, attemptsDone, start, fmt.Errorf("forwarding request to %s: %w", req.URL, lastErr)
	}

	return lastResp, attemptsDone, start, nil
}

// recordCircuitOutcome records a circuit-breaker success or failure based on
// the final response status code.
func (p *Proxy) recordCircuitOutcome(ctx context.Context, match domainegress.RouteMatch, resp *http.Response) {
	if !match.Matched || p.cfg.CircuitBreakers == nil {
		return
	}
	if isFailureStatus(resp.StatusCode) {
		p.cfg.CircuitBreakers.RecordFailure(ctx, match.Route)
	} else {
		p.cfg.CircuitBreakers.RecordSuccess(ctx, match.Route)
	}
}

// recordEgressSuccess records metrics, emits the egress.response event, and
// sets the span status for a successful upstream response.
func (p *Proxy) recordEgressSuccess(
	ctx context.Context,
	span ports.Span,
	match domainegress.RouteMatch,
	req domainegress.EgressRequest,
	routeName string,
	attempts int,
	duration time.Duration,
	resp *http.Response,
) {
	if p.cfg.Metrics != nil {
		p.cfg.Metrics.IncEgressRequestTotal(routeName, req.Method, strconv.Itoa(resp.StatusCode))
		p.cfg.Metrics.ObserveEgressDuration(routeName, req.Method, duration)
	}
	if p.cfg.EventLogger != nil {
		_ = p.cfg.EventLogger.Log(ctx, events.NewEgressResponse(events.EgressResponseParams{
			Route:           routeName,
			Method:          req.Method,
			URL:             req.URL,
			StatusCode:      resp.StatusCode,
			DurationSeconds: duration.Seconds(),
			Attempts:        attempts,
			TraceID:         traceIDFromContext(ctx),
		}))
	}
	if span != nil {
		span.SetAttributes(
			ports.Attribute{Key: "http.response.status_code", Value: strconv.Itoa(resp.StatusCode)},
		)
		if resp.StatusCode >= 500 {
			span.SetStatus(ports.SpanStatusError, http.StatusText(resp.StatusCode))
		} else {
			span.SetStatus(ports.SpanStatusOK, "")
		}
	}
}

// buildEgressResponse strips sensitive response headers and wraps the upstream
// response into a domainegress.EgressResponse, stamping the attempt count.
func (p *Proxy) buildEgressResponse(
	resp *http.Response,
	match domainegress.RouteMatch,
	attempts int,
	duration time.Duration,
) (domainegress.EgressResponse, error) {
	var respHeaders http.Header
	if match.Matched {
		respHeaders = match.Route.Headers().ApplyToResponse(resp.Header)
	} else {
		respHeaders = domainegress.HeadersConfig{}.ApplyToResponse(resp.Header)
	}

	egressResp, err := domainegress.NewEgressResponse(resp.StatusCode, respHeaders, resp.Body, duration)
	if err != nil {
		resp.Body.Close() //nolint:errcheck
		return domainegress.EgressResponse{}, fmt.Errorf("building egress response: %w", err)
	}

	egressResp.Attempts = attempts
	return egressResp, nil
}
