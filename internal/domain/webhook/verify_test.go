package webhook_test

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // Twilio requires SHA-1 per its API specification
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/webhook"
)

// fakeHeaders is a simple map-backed implementation of webhook.Headers.
type fakeHeaders map[string]string

func (h fakeHeaders) Get(key string) string { return h[key] }

// helpers — compute expected signatures for tests.

func hmacSHA256Hex(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message)) //nolint:errcheck
	return hex.EncodeToString(mac.Sum(nil))
}

func hmacSHA1Base64(secret, message string) string {
	mac := hmac.New(sha1.New, []byte(secret)) //nolint:gosec
	mac.Write([]byte(message))                //nolint:errcheck
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// VerifyStripe
// ---------------------------------------------------------------------------

func TestVerifyStripe(t *testing.T) {
	const secret = "stripe_test_secret"
	const body = `{"type":"checkout.session.completed"}`

	tests := []struct {
		name    string
		headers fakeHeaders
		body    []byte
		wantErr error
	}{
		{
			name: "valid signature",
			headers: func() fakeHeaders {
				ts := "1614556800"
				sig := hmacSHA256Hex(secret, ts+"."+body)
				return fakeHeaders{"Stripe-Signature": "t=" + ts + ",v1=" + sig}
			}(),
			body:    []byte(body),
			wantErr: nil,
		},
		{
			name:    "missing header",
			headers: fakeHeaders{},
			body:    []byte(body),
			wantErr: webhook.ErrMissingSignature,
		},
		{
			name:    "missing timestamp field",
			headers: fakeHeaders{"Stripe-Signature": "v1=abc123"},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidFormat,
		},
		{
			name:    "missing v1 field",
			headers: fakeHeaders{"Stripe-Signature": "t=1614556800"},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidFormat,
		},
		{
			name:    "wrong signature",
			headers: fakeHeaders{"Stripe-Signature": "t=1614556800,v1=deadbeef"},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidSignature,
		},
		{
			name: "multiple v1 values — one valid",
			headers: func() fakeHeaders {
				ts := "1614556800"
				sig := hmacSHA256Hex(secret, ts+"."+body)
				return fakeHeaders{"Stripe-Signature": "t=" + ts + ",v1=wrongsig," + "v1=" + sig}
			}(),
			body:    []byte(body),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhook.VerifyStripe(secret, tt.headers, tt.body)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("VerifyStripe() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VerifyGitHub
// ---------------------------------------------------------------------------

func TestVerifyGitHub(t *testing.T) {
	const secret = "github_test_secret"
	const body = `{"action":"opened"}`

	tests := []struct {
		name    string
		headers fakeHeaders
		body    []byte
		wantErr error
	}{
		{
			name:    "valid signature",
			headers: fakeHeaders{"X-Hub-Signature-256": "sha256=" + hmacSHA256Hex(secret, body)},
			body:    []byte(body),
			wantErr: nil,
		},
		{
			name:    "missing header",
			headers: fakeHeaders{},
			body:    []byte(body),
			wantErr: webhook.ErrMissingSignature,
		},
		{
			name:    "missing sha256 prefix",
			headers: fakeHeaders{"X-Hub-Signature-256": hmacSHA256Hex(secret, body)},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidFormat,
		},
		{
			name:    "wrong signature",
			headers: fakeHeaders{"X-Hub-Signature-256": "sha256=deadbeef"},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidSignature,
		},
		{
			name:    "empty body",
			headers: fakeHeaders{"X-Hub-Signature-256": "sha256=" + hmacSHA256Hex(secret, "")},
			body:    []byte{},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhook.VerifyGitHub(secret, tt.headers, tt.body)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("VerifyGitHub() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VerifySlack
// ---------------------------------------------------------------------------

func TestVerifySlack(t *testing.T) {
	const secret = "slack_test_secret"
	const ts = "1531420618"
	const body = "token=abc&team_id=T123&text=hello"

	tests := []struct {
		name    string
		headers fakeHeaders
		body    []byte
		wantErr error
	}{
		{
			name: "valid signature",
			headers: fakeHeaders{
				"X-Slack-Signature":         "v0=" + hmacSHA256Hex(secret, "v0:"+ts+":"+body),
				"X-Slack-Request-Timestamp": ts,
			},
			body:    []byte(body),
			wantErr: nil,
		},
		{
			name:    "missing signature header",
			headers: fakeHeaders{"X-Slack-Request-Timestamp": ts},
			body:    []byte(body),
			wantErr: webhook.ErrMissingSignature,
		},
		{
			name:    "missing timestamp header",
			headers: fakeHeaders{"X-Slack-Signature": "v0=abc"},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidFormat,
		},
		{
			name:    "missing v0 prefix",
			headers: fakeHeaders{"X-Slack-Signature": "abc", "X-Slack-Request-Timestamp": ts},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidFormat,
		},
		{
			name: "wrong signature",
			headers: fakeHeaders{
				"X-Slack-Signature":         "v0=deadbeef",
				"X-Slack-Request-Timestamp": ts,
			},
			body:    []byte(body),
			wantErr: webhook.ErrInvalidSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhook.VerifySlack(secret, tt.headers, tt.body)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("VerifySlack() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VerifyTwilio
// ---------------------------------------------------------------------------

func TestVerifyTwilio(t *testing.T) {
	const secret = "twilio_test_secret"
	const rawURL = "https://example.com/webhooks/sms"

	tests := []struct {
		name       string
		headers    fakeHeaders
		fullURL    string
		formParams url.Values
		wantErr    error
	}{
		{
			name: "valid signature — no params",
			headers: fakeHeaders{
				"X-Twilio-Signature": hmacSHA1Base64(secret, rawURL),
			},
			fullURL:    rawURL,
			formParams: url.Values{},
			wantErr:    nil,
		},
		{
			name: "valid signature — with sorted params",
			headers: func() fakeHeaders {
				// Sorted: From, To — buildTwilioPayload sorts lexicographically
				payload := rawURL + "From" + "+15005550002" + "To" + "+15005550001"
				return fakeHeaders{"X-Twilio-Signature": hmacSHA1Base64(secret, payload)}
			}(),
			fullURL:    rawURL,
			formParams: url.Values{"To": []string{"+15005550001"}, "From": []string{"+15005550002"}},
			wantErr:    nil,
		},
		{
			name:       "missing header",
			headers:    fakeHeaders{},
			fullURL:    rawURL,
			formParams: url.Values{},
			wantErr:    webhook.ErrMissingSignature,
		},
		{
			name: "wrong signature",
			headers: fakeHeaders{
				"X-Twilio-Signature": "badsignature==",
			},
			fullURL:    rawURL,
			formParams: url.Values{},
			wantErr:    webhook.ErrInvalidSignature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhook.VerifyTwilio(secret, tt.headers, tt.fullURL, tt.formParams)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("VerifyTwilio() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VerifyGeneric
// ---------------------------------------------------------------------------

func TestVerifyGeneric(t *testing.T) {
	const secret = "generic_test_secret"
	const headerName = "X-My-Webhook-Sig"
	const body = `{"event":"test"}`

	tests := []struct {
		name       string
		headers    fakeHeaders
		body       []byte
		headerName string
		wantErr    error
	}{
		{
			name:       "valid signature",
			headers:    fakeHeaders{headerName: hmacSHA256Hex(secret, body)},
			body:       []byte(body),
			headerName: headerName,
			wantErr:    nil,
		},
		{
			name:       "missing header",
			headers:    fakeHeaders{},
			body:       []byte(body),
			headerName: headerName,
			wantErr:    webhook.ErrMissingSignature,
		},
		{
			name:       "wrong signature",
			headers:    fakeHeaders{headerName: "deadbeef"},
			body:       []byte(body),
			headerName: headerName,
			wantErr:    webhook.ErrInvalidSignature,
		},
		{
			name:       "empty header name",
			headers:    fakeHeaders{},
			body:       []byte(body),
			headerName: "",
			wantErr:    webhook.ErrInvalidFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhook.VerifyGeneric(secret, tt.headers, tt.body, tt.headerName)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("VerifyGeneric() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Verify (dispatcher)
// ---------------------------------------------------------------------------

func TestVerify(t *testing.T) {
	const secret = "dispatch_secret"
	const body = `{"data":"test"}`

	githubSig := "sha256=" + hmacSHA256Hex(secret, body)

	tests := []struct {
		name    string
		cfg     webhook.VerifyConfig
		headers fakeHeaders
		body    []byte
		wantErr error
	}{
		{
			name:    "github provider dispatched correctly",
			cfg:     webhook.VerifyConfig{Provider: webhook.ProviderGitHub, Secret: secret},
			headers: fakeHeaders{"X-Hub-Signature-256": githubSig},
			body:    []byte(body),
			wantErr: nil,
		},
		{
			name:    "unknown provider returns error",
			cfg:     webhook.VerifyConfig{Provider: "nonexistent", Secret: secret},
			headers: fakeHeaders{},
			body:    []byte(body),
			wantErr: nil, // fmt.Errorf, not a sentinel — just check non-nil
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhook.Verify(tt.cfg, tt.headers, tt.body, "", url.Values{})
			if tt.name == "unknown provider returns error" {
				if err == nil {
					t.Error("Verify() expected error for unknown provider, got nil")
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Verify() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
