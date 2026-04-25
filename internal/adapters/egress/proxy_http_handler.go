package egress

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// parseIncomingRequest extracts the target URL and builds an EgressRequest from
// the raw HTTP request. It strips hop-by-hop headers and the X-Egress-URL proxy
// header from the outbound headers before constructing the request.
// Returns the egress request, the resolved target URL, and any error.
func (p *Proxy) parseIncomingRequest(r *http.Request) (domainegress.EgressRequest, string, error) {
	targetURL, err := p.resolveTargetURL(r)
	if err != nil {
		return domainegress.EgressRequest{}, "", err
	}

	outHeaders := cloneAndStripHopByHop(r.Header)
	outHeaders.Del(headerEgressURL)

	egressReq, err := domainegress.NewEgressRequest(r.Method, targetURL, outHeaders, r.Body)
	if err != nil {
		return domainegress.EgressRequest{}, targetURL, fmt.Errorf("invalid egress request: %w", err)
	}
	return egressReq, targetURL, nil
}

// retryAfterForRequest resolves the matched route for the request and computes
// the Retry-After header value in seconds. Falls back to "1" when the route
// cannot be resolved or the rate limiter cannot provide the value.
func (p *Proxy) retryAfterForRequest(r *http.Request, targetURL string) string {
	retryAfter := "1"
	if p.cfg.RateLimiters == nil {
		return retryAfter
	}
	egressReq2, reqErr := domainegress.NewEgressRequest(r.Method, targetURL, nil, nil)
	if reqErr != nil {
		return retryAfter
	}
	m2, resolveErr := p.resolver.Resolve(r.Context(), egressReq2)
	if resolveErr != nil || !m2.Matched {
		return retryAfter
	}
	secs, raErr := p.cfg.RateLimiters.RetryAfterSeconds(m2.Route)
	if raErr != nil {
		return retryAfter
	}
	return retryAfterHeader(secs)
}

// writeEgressErrorResponse maps known sentinel errors to appropriate HTTP
// status codes and writes the error response. The mapping is:
//
//	ErrDeniedByPolicy         → 403
//	ErrInsecureURL            → 400
//	ErrRequestBodyTooLarge    → 413
//	ErrCircuitOpen            → 503
//	ErrRateLimitExceeded      → 429 + Retry-After
//	ErrResponseValidationFailed → 502
//	ErrMTLSHandshakeFailed    → 502
//	*SSRFBlockedError         → 403
//	timeout error             → 504
//	fallback                  → 502
func (p *Proxy) writeEgressErrorResponse(w http.ResponseWriter, r *http.Request, targetURL string, err error) {
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
		retryAfter := p.retryAfterForRequest(r, targetURL)
		p.logger.WarnContext(r.Context(), "egress rate limit exceeded — request rejected",
			slog.String("target", targetURL),
			slog.String("method", r.Method),
			slog.String("retry_after", retryAfter),
		)
		w.Header().Set("Retry-After", retryAfter)
		http.Error(w, "429 Too Many Requests: egress rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	if errors.Is(err, ErrResponseValidationFailed) {
		p.logger.WarnContext(r.Context(), "egress.response_invalid",
			slog.String("event_type", "egress.response_invalid"),
			slog.String("target", targetURL),
			slog.String("method", r.Method),
			slog.String("err", err.Error()),
		)
		http.Error(w, "502 Bad Gateway: "+err.Error(), http.StatusBadGateway)
		return
	}
	if errors.Is(err, ErrMTLSHandshakeFailed) {
		p.logger.ErrorContext(r.Context(), "egress.mtls_error",
			slog.String("event_type", "egress.mtls_error"),
			slog.String("target", targetURL),
			slog.String("method", r.Method),
			slog.String("err", err.Error()),
		)
		http.Error(w, "502 Bad Gateway: mTLS handshake failed", http.StatusBadGateway)
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
}

// writeEgressResponse writes the upstream response back to the HTTP client.
// It copies response headers, announces the truncation trailer when a size
// limit is active, writes the status code, and streams the response body.
func (p *Proxy) writeEgressResponse(
	w http.ResponseWriter,
	r *http.Request,
	egressReq domainegress.EgressRequest,
	egressResp domainegress.EgressResponse,
	targetURL string,
) {
	respBody, _ := egressResp.BodyRef.(io.ReadCloser)

	respSizeLimit := p.responseSizeLimitFor(egressReq)

	respHeaders := cloneAndStripHopByHop(egressResp.Header)
	if respSizeLimit > 0 {
		respHeaders.Del("Content-Length")
	}
	for key, vals := range respHeaders {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	if egressResp.Attempts > 0 {
		w.Header().Set(headerEgressAttempts, fmt.Sprintf("%d", egressResp.Attempts))
	}
	if respSizeLimit > 0 {
		w.Header().Add("Trailer", headerResponseTruncated)
	}
	w.WriteHeader(egressResp.StatusCode)

	if respBody != nil {
		p.streamResponseBody(w, r, targetURL, respBody, respSizeLimit)
	}
}

// streamResponseBody copies the response body to w. When limit > 0 it copies
// at most limit bytes, then probes for truncation and sets the
// X-Egress-Response-Truncated trailer and logs a warning when the body exceeded
// the limit.
func (p *Proxy) streamResponseBody(w http.ResponseWriter, r *http.Request, targetURL string, body io.ReadCloser, limit int64) {
	defer body.Close() //nolint:errcheck

	if limit > 0 {
		written, copyErr := io.Copy(w, io.LimitReader(body, limit))
		var probe [1]byte
		n, _ := body.Read(probe[:])
		if n > 0 {
			p.logger.WarnContext(r.Context(), "egress.body_size_exceeded",
				slog.String("event_type", "egress.body_size_exceeded"),
				slog.String("kind", "response"),
				slog.String("target", targetURL),
				slog.String("method", r.Method),
				slog.Int64("limit_bytes", limit),
				slog.Int64("bytes_written", written),
			)
			w.Header().Set(headerResponseTruncated, "true")
		}
		if copyErr != nil {
			p.logger.WarnContext(r.Context(), "writing egress response body", "err", copyErr)
		}
		return
	}

	if _, err := io.Copy(w, body); err != nil {
		p.logger.WarnContext(r.Context(), "writing egress response body", "err", err)
	}
}
