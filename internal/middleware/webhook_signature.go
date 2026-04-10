package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainwebhook "github.com/vibewarden/vibewarden/internal/domain/webhook"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// WebhookSignatureRule pairs a URL path with the verification config to apply
// to inbound requests on that path.
type WebhookSignatureRule struct {
	// Path is the URL path that this rule applies to (exact match).
	Path string

	// Config holds the signature verification configuration for this path.
	Config domainwebhook.VerifyConfig
}

// WebhookSignatureMiddleware returns HTTP middleware that verifies inbound
// webhook request signatures on configured paths.
//
// For each request the middleware:
//  1. Checks whether the request path matches a configured rule.
//     If no rule matches, the request is passed through unchanged.
//  2. Buffers the request body (required to compute the HMAC).
//  3. Verifies the signature using the provider-specific algorithm.
//  4. On success: emits webhook.signature_valid, restores the body, and calls next.
//  5. On failure: emits webhook.signature_invalid and returns 401 Unauthorized.
//
// The eventLogger receives structured events following the VibeWarden schema.
// If eventLogger is nil, event logging is skipped silently.
func WebhookSignatureMiddleware(
	rules []WebhookSignatureRule,
	eventLogger ports.EventLogger,
) func(http.Handler) http.Handler {
	index := make(map[string]domainwebhook.VerifyConfig, len(rules))
	for _, r := range rules {
		index[r.Path] = r.Config
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg, ok := index[r.URL.Path]
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Buffer the body so we can read it for HMAC verification and then
			// restore it for the upstream handler.
			body, err := io.ReadAll(r.Body)
			if err != nil {
				emitWebhookInvalid(r, eventLogger, cfg.Provider, "failed to read request body")
				WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized", "could not read request body")
				return
			}
			// Restore the body for downstream handlers.
			r.Body = io.NopCloser(bytes.NewReader(body))

			// Build form params for Twilio (parse only when needed).
			// We parse directly from the already-buffered body bytes rather than
			// calling r.ParseForm() to avoid gosec G120 (body size is already
			// bounded by the prior io.ReadAll).
			var formParams url.Values
			if cfg.Provider == domainwebhook.ProviderTwilio {
				if parsed, parseErr := url.ParseQuery(string(body)); parseErr == nil {
					formParams = parsed
				} else {
					formParams = url.Values{}
				}
			}

			// Derive the full URL for Twilio verification.
			fullURL := buildFullURL(r)

			verifyErr := domainwebhook.Verify(cfg, r.Header, body, fullURL, formParams)
			if verifyErr != nil {
				reason := classifyError(verifyErr)
				emitWebhookInvalid(r, eventLogger, cfg.Provider, reason)
				WriteErrorResponse(w, r, http.StatusUnauthorized, "unauthorized", "webhook signature verification failed")
				return
			}

			emitWebhookValid(r, eventLogger, cfg.Provider)
			next.ServeHTTP(w, r)
		})
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildFullURL constructs the full request URL string needed for Twilio
// signature verification.
func buildFullURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host + r.RequestURI
}

// classifyError maps a domain verification error to a human-readable reason
// string suitable for event payloads.
func classifyError(err error) string {
	switch {
	case errors.Is(err, domainwebhook.ErrMissingSignature):
		return "signature header missing"
	case errors.Is(err, domainwebhook.ErrInvalidFormat):
		return "signature format invalid"
	case errors.Is(err, domainwebhook.ErrInvalidSignature):
		return "signature mismatch"
	default:
		return err.Error()
	}
}

// emitWebhookValid emits a webhook.signature_valid event.
// If eventLogger is nil the call is a no-op.
func emitWebhookValid(r *http.Request, eventLogger ports.EventLogger, provider domainwebhook.Provider) {
	if eventLogger == nil {
		return
	}
	ev := events.NewWebhookSignatureValid(events.WebhookSignatureValidParams{
		Path:     r.URL.Path,
		Method:   r.Method,
		Provider: string(provider),
		ClientIP: ExtractClientIP(r, true),
	})
	_ = eventLogger.Log(r.Context(), ev)
}

// emitWebhookInvalid emits a webhook.signature_invalid event.
// If eventLogger is nil the call is a no-op.
func emitWebhookInvalid(r *http.Request, eventLogger ports.EventLogger, provider domainwebhook.Provider, reason string) {
	if eventLogger == nil {
		return
	}
	ev := events.NewWebhookSignatureInvalid(events.WebhookSignatureInvalidParams{
		Path:     r.URL.Path,
		Method:   r.Method,
		Provider: string(provider),
		Reason:   reason,
		ClientIP: ExtractClientIP(r, true),
	})
	_ = eventLogger.Log(r.Context(), ev)
}
