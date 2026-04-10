// Package webhook provides domain logic for verifying inbound webhook request
// signatures. It supports Stripe, GitHub, Slack, Twilio, and a generic
// HMAC-SHA256 format. All verification is done using Go stdlib crypto/hmac
// with no external dependencies.
package webhook

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio requires SHA-1 per its API specification
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Provider identifies the webhook signature format to use for verification.
type Provider string

const (
	// ProviderStripe verifies Stripe webhook signatures.
	// Header: Stripe-Signature (format: t=timestamp,v1=hex_hmac)
	// Signed payload: "<timestamp>.<raw_body>"
	// Algorithm: HMAC-SHA256
	ProviderStripe Provider = "stripe"

	// ProviderGitHub verifies GitHub webhook signatures.
	// Header: X-Hub-Signature-256 (format: sha256=hex_hmac)
	// Signed payload: raw request body
	// Algorithm: HMAC-SHA256
	ProviderGitHub Provider = "github"

	// ProviderSlack verifies Slack webhook signatures.
	// Headers: X-Slack-Signature (format: v0=hex_hmac), X-Slack-Request-Timestamp
	// Signed payload: "v0:<timestamp>:<raw_body>"
	// Algorithm: HMAC-SHA256
	ProviderSlack Provider = "slack"

	// ProviderTwilio verifies Twilio webhook signatures.
	// Header: X-Twilio-Signature (base64-encoded HMAC-SHA1)
	// Signed payload: full request URL + sorted POST params (key+value concatenated)
	// Algorithm: HMAC-SHA1
	ProviderTwilio Provider = "twilio"

	// ProviderGeneric verifies a configurable HMAC-SHA256 signature.
	// The header name is operator-configurable. The signed payload is the raw body.
	// Algorithm: HMAC-SHA256, hex-encoded
	ProviderGeneric Provider = "generic"
)

// ErrMissingSignature is returned when the expected signature header is absent.
var ErrMissingSignature = errors.New("signature header missing")

// ErrInvalidSignature is returned when the signature does not match the
// computed HMAC.
var ErrInvalidSignature = errors.New("signature mismatch")

// ErrInvalidFormat is returned when the signature header value does not conform
// to the expected format for the provider.
var ErrInvalidFormat = errors.New("signature format invalid")

// VerifyConfig holds the configuration needed to verify a single webhook path.
type VerifyConfig struct {
	// Provider selects the signature format.
	Provider Provider

	// Secret is the shared HMAC secret. For env-var references this is the
	// resolved value (not the "${VAR}" reference — that is expanded by the
	// config layer before constructing VerifyConfig).
	Secret string

	// Header is the custom header name used when Provider is ProviderGeneric.
	// Ignored for all other providers.
	Header string
}

// Headers is an interface for reading HTTP request headers. It matches the
// subset of http.Header needed by the verifier so that domain code remains
// free of net/http imports.
type Headers interface {
	// Get returns the first value associated with the given key, or "" if
	// the header is not set.
	Get(key string) string
}

// VerifyStripe verifies a Stripe webhook signature.
//
// Stripe sends the signature in the Stripe-Signature header with the format:
//
//	t=<timestamp>,v1=<hex_hmac>[,v1=<additional_hmac>...]
//
// The signed payload is "<timestamp>.<raw_body>".
// This function accepts the signature if at least one v1 signature value
// matches the computed HMAC.
func VerifyStripe(secret string, headers Headers, body []byte) error {
	sig := headers.Get("Stripe-Signature")
	if sig == "" {
		return ErrMissingSignature
	}

	timestamp, signatures, err := parseStripeSig(sig)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidFormat, err.Error())
	}

	signed := timestamp + "." + string(body)
	computed := computeHMACSHA256Hex([]byte(secret), []byte(signed))

	for _, s := range signatures {
		if hmac.Equal([]byte(computed), []byte(s)) {
			return nil
		}
	}
	return ErrInvalidSignature
}

// VerifyGitHub verifies a GitHub webhook signature.
//
// GitHub sends the signature in the X-Hub-Signature-256 header with the format:
//
//	sha256=<hex_hmac>
//
// The signed payload is the raw request body.
func VerifyGitHub(secret string, headers Headers, body []byte) error {
	sig := headers.Get("X-Hub-Signature-256")
	if sig == "" {
		return ErrMissingSignature
	}

	const prefix = "sha256="
	if !strings.HasPrefix(sig, prefix) {
		return fmt.Errorf("%w: expected sha256= prefix", ErrInvalidFormat)
	}
	received := strings.TrimPrefix(sig, prefix)
	computed := computeHMACSHA256Hex([]byte(secret), body)

	if !hmac.Equal([]byte(computed), []byte(received)) {
		return ErrInvalidSignature
	}
	return nil
}

// VerifySlack verifies a Slack webhook signature.
//
// Slack sends the signature in the X-Slack-Signature header with the format:
//
//	v0=<hex_hmac>
//
// and the request timestamp in X-Slack-Request-Timestamp.
// The signed payload is "v0:<timestamp>:<raw_body>".
func VerifySlack(secret string, headers Headers, body []byte) error {
	sig := headers.Get("X-Slack-Signature")
	if sig == "" {
		return ErrMissingSignature
	}
	timestamp := headers.Get("X-Slack-Request-Timestamp")
	if timestamp == "" {
		return fmt.Errorf("%w: X-Slack-Request-Timestamp header missing", ErrInvalidFormat)
	}

	const prefix = "v0="
	if !strings.HasPrefix(sig, prefix) {
		return fmt.Errorf("%w: expected v0= prefix", ErrInvalidFormat)
	}
	received := strings.TrimPrefix(sig, prefix)

	signed := "v0:" + timestamp + ":" + string(body)
	computed := computeHMACSHA256Hex([]byte(secret), []byte(signed))

	if !hmac.Equal([]byte(computed), []byte(received)) {
		return ErrInvalidSignature
	}
	return nil
}

// VerifyTwilio verifies a Twilio webhook signature.
//
// Twilio sends the signature in X-Twilio-Signature as a base64-encoded
// HMAC-SHA1 of (fullURL + sorted-POST-params). For POST requests with
// application/x-www-form-urlencoded bodies, the sorted params are appended to
// the URL: each param key is appended followed by its value (no separator).
// Keys are sorted lexicographically.
//
// fullURL must be the complete request URL including scheme, host, and path.
// formParams are the parsed form parameters (from the POST body).
func VerifyTwilio(secret string, headers Headers, fullURL string, formParams url.Values) error {
	sig := headers.Get("X-Twilio-Signature")
	if sig == "" {
		return ErrMissingSignature
	}

	payload := buildTwilioPayload(fullURL, formParams)
	computed := computeHMACSHA1Base64([]byte(secret), []byte(payload))

	if !hmac.Equal([]byte(computed), []byte(sig)) {
		return ErrInvalidSignature
	}
	return nil
}

// VerifyGeneric verifies a generic HMAC-SHA256 webhook signature.
//
// The signature is read from the header named by headerName and must be a
// lowercase hex-encoded HMAC-SHA256 of the raw request body.
func VerifyGeneric(secret string, headers Headers, body []byte, headerName string) error {
	if headerName == "" {
		return fmt.Errorf("%w: header name is required for generic provider", ErrInvalidFormat)
	}
	sig := headers.Get(headerName)
	if sig == "" {
		return ErrMissingSignature
	}

	computed := computeHMACSHA256Hex([]byte(secret), body)
	if !hmac.Equal([]byte(computed), []byte(sig)) {
		return ErrInvalidSignature
	}
	return nil
}

// Verify dispatches signature verification to the correct provider-specific
// function based on cfg.Provider. For ProviderTwilio the caller must supply
// fullURL and formParams; for all other providers those arguments are ignored.
func Verify(cfg VerifyConfig, headers Headers, body []byte, fullURL string, formParams url.Values) error {
	switch cfg.Provider {
	case ProviderStripe:
		return VerifyStripe(cfg.Secret, headers, body)
	case ProviderGitHub:
		return VerifyGitHub(cfg.Secret, headers, body)
	case ProviderSlack:
		return VerifySlack(cfg.Secret, headers, body)
	case ProviderTwilio:
		return VerifyTwilio(cfg.Secret, headers, fullURL, formParams)
	case ProviderGeneric:
		return VerifyGeneric(cfg.Secret, headers, body, cfg.Header)
	default:
		return fmt.Errorf("unknown webhook provider %q", cfg.Provider)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers — pure functions, no side effects.
// ---------------------------------------------------------------------------

// computeHMACSHA256Hex computes an HMAC-SHA256 of message using key and
// returns the result as a lowercase hex string.
func computeHMACSHA256Hex(key, message []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(message) //nolint:errcheck // hash.Hash.Write never returns an error
	return hex.EncodeToString(mac.Sum(nil))
}

// computeHMACSHA1Base64 computes an HMAC-SHA1 of message using key and
// returns the result as a base64-encoded string (standard encoding with padding).
func computeHMACSHA1Base64(key, message []byte) string {
	mac := hmac.New(sha1.New, key) //nolint:gosec // Twilio requires SHA-1
	mac.Write(message)             //nolint:errcheck // hash.Hash.Write never returns an error
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// parseStripeSig parses a Stripe-Signature header value into a timestamp
// string and a slice of v1 signature hex strings.
// Format: "t=<timestamp>,v1=<hex>[,v1=<hex>...]"
func parseStripeSig(sig string) (timestamp string, signatures []string, err error) {
	parts := strings.Split(sig, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			signatures = append(signatures, kv[1])
		}
	}
	if timestamp == "" {
		return "", nil, errors.New("missing t= timestamp field")
	}
	if len(signatures) == 0 {
		return "", nil, errors.New("missing v1= signature field")
	}
	return timestamp, signatures, nil
}

// buildTwilioPayload constructs the signing payload for Twilio signature
// verification: the full URL followed by each form param key+value pair in
// lexicographic key order.
func buildTwilioPayload(fullURL string, params url.Values) string {
	if len(params) == 0 {
		return fullURL
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(params.Get(k))
	}
	return sb.String()
}
