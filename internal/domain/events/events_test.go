package events_test

import (
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// maxAISummaryLen is the maximum allowed length of an AISummary, as defined
// in the JSON schema.
const maxAISummaryLen = 200

// assertEvent verifies the invariants that every Event must satisfy regardless
// of its type.
func assertEvent(t *testing.T, e events.Event, wantType string) {
	t.Helper()

	if e.SchemaVersion != "v1" {
		t.Errorf("SchemaVersion = %q, want %q", e.SchemaVersion, "v1")
	}
	if e.EventType != wantType {
		t.Errorf("EventType = %q, want %q", e.EventType, wantType)
	}
	if e.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}
	if e.Timestamp.Location() != time.UTC {
		t.Errorf("Timestamp location = %v, want UTC", e.Timestamp.Location())
	}
	if e.AISummary == "" {
		t.Error("AISummary is empty")
	}
	if len(e.AISummary) > maxAISummaryLen {
		t.Errorf("AISummary length = %d, want <= %d; value: %q", len(e.AISummary), maxAISummaryLen, e.AISummary)
	}
	if e.Payload == nil {
		t.Error("Payload is nil")
	}
}

// requirePayloadKey asserts that the payload map contains the given key.
func requirePayloadKey(t *testing.T, payload map[string]any, key string) {
	t.Helper()
	if _, ok := payload[key]; !ok {
		t.Errorf("Payload missing key %q", key)
	}
}

// requirePayloadString asserts that the payload map contains key with the
// expected string value.
func requirePayloadString(t *testing.T, payload map[string]any, key, want string) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("Payload missing key %q", key)
		return
	}
	got, ok := v.(string)
	if !ok {
		t.Errorf("Payload[%q] type = %T, want string", key, v)
		return
	}
	if got != want {
		t.Errorf("Payload[%q] = %q, want %q", key, got, want)
	}
}

// requirePayloadBool asserts that the payload map contains key with the
// expected bool value.
func requirePayloadBool(t *testing.T, payload map[string]any, key string, want bool) {
	t.Helper()
	v, ok := payload[key]
	if !ok {
		t.Errorf("Payload missing key %q", key)
		return
	}
	got, ok := v.(bool)
	if !ok {
		t.Errorf("Payload[%q] type = %T, want bool", key, v)
		return
	}
	if got != want {
		t.Errorf("Payload[%q] = %v, want %v", key, got, want)
	}
}

// requireSummaryContains asserts that the AISummary contains the given substring.
func requireSummaryContains(t *testing.T, summary, substr string) {
	t.Helper()
	if !strings.Contains(summary, substr) {
		t.Errorf("AISummary %q does not contain %q", summary, substr)
	}
}

// --- proxy.started ---

func TestNewProxyStarted(t *testing.T) {
	tests := []struct {
		name           string
		params         events.ProxyStartedParams
		wantListen     string
		wantUpstream   string
		wantTLSEnabled bool
		wantTLSProv    string
		wantSecHeaders bool
		wantVersion    string
		wantSummary    string
	}{
		{
			name: "tls enabled with security headers",
			params: events.ProxyStartedParams{
				ListenAddr:             ":8443",
				UpstreamAddr:           "localhost:3000",
				TLSEnabled:             true,
				TLSProvider:            "letsencrypt",
				SecurityHeadersEnabled: true,
				Version:                "1.0.0",
			},
			wantListen:     ":8443",
			wantUpstream:   "localhost:3000",
			wantTLSEnabled: true,
			wantTLSProv:    "letsencrypt",
			wantSecHeaders: true,
			wantVersion:    "1.0.0",
			wantSummary:    ":8443",
		},
		{
			name: "tls disabled no security headers",
			params: events.ProxyStartedParams{
				ListenAddr:             ":8080",
				UpstreamAddr:           "localhost:4000",
				TLSEnabled:             false,
				TLSProvider:            "",
				SecurityHeadersEnabled: false,
				Version:                "0.1.0",
			},
			wantListen:     ":8080",
			wantUpstream:   "localhost:4000",
			wantTLSEnabled: false,
			wantTLSProv:    "",
			wantSecHeaders: false,
			wantVersion:    "0.1.0",
			wantSummary:    "localhost:4000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewProxyStarted(tt.params)

			assertEvent(t, e, events.EventTypeProxyStarted)
			requireSummaryContains(t, e.AISummary, tt.wantSummary)
			requirePayloadString(t, e.Payload, "listen", tt.wantListen)
			requirePayloadString(t, e.Payload, "upstream", tt.wantUpstream)
			requirePayloadBool(t, e.Payload, "tls_enabled", tt.wantTLSEnabled)
			requirePayloadString(t, e.Payload, "tls_provider", tt.wantTLSProv)
			requirePayloadBool(t, e.Payload, "security_headers_enabled", tt.wantSecHeaders)
			requirePayloadString(t, e.Payload, "version", tt.wantVersion)
		})
	}
}

// --- proxy.kratos_flow ---

func TestNewProxyKratosFlow(t *testing.T) {
	tests := []struct {
		name   string
		params events.ProxyKratosFlowParams
	}{
		{
			name: "login browser flow",
			params: events.ProxyKratosFlowParams{
				Method: "GET",
				Path:   "/self-service/login/browser",
			},
		},
		{
			name: "registration API",
			params: events.ProxyKratosFlowParams{
				Method: "POST",
				Path:   "/.ory/kratos/public/self-service/registration",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewProxyKratosFlow(tt.params)

			assertEvent(t, e, events.EventTypeProxyKratosFlow)
			requireSummaryContains(t, e.AISummary, "Kratos")
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
		})
	}
}

// --- auth.success ---

func TestNewAuthSuccess(t *testing.T) {
	tests := []struct {
		name   string
		params events.AuthSuccessParams
	}{
		{
			name: "standard authenticated GET",
			params: events.AuthSuccessParams{
				Method:     "GET",
				Path:       "/api/data",
				SessionID:  "sess-abc123",
				IdentityID: "id-xyz456",
				Email:      "alice@example.com",
			},
		},
		{
			name: "authenticated POST to admin",
			params: events.AuthSuccessParams{
				Method:     "POST",
				Path:       "/admin/users",
				SessionID:  "sess-def789",
				IdentityID: "id-admin001",
				Email:      "admin@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewAuthSuccess(tt.params)

			assertEvent(t, e, events.EventTypeAuthSuccess)
			requireSummaryContains(t, e.AISummary, tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "session_id", tt.params.SessionID)
			requirePayloadString(t, e.Payload, "identity_id", tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "email", tt.params.Email)
		})
	}
}

// --- auth.failed ---

func TestNewAuthFailed(t *testing.T) {
	tests := []struct {
		name   string
		params events.AuthFailedParams
	}{
		{
			name: "missing cookie",
			params: events.AuthFailedParams{
				Method: "GET",
				Path:   "/dashboard",
				Reason: "missing session cookie",
				Detail: "",
			},
		},
		{
			name: "provider unavailable",
			params: events.AuthFailedParams{
				Method: "POST",
				Path:   "/api/submit",
				Reason: "auth provider unavailable",
				Detail: "connection refused",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewAuthFailed(tt.params)

			assertEvent(t, e, events.EventTypeAuthFailed)
			requireSummaryContains(t, e.AISummary, tt.params.Reason)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			requirePayloadString(t, e.Payload, "detail", tt.params.Detail)
		})
	}
}

// --- rate_limit.hit ---

func TestNewRateLimitHit(t *testing.T) {
	tests := []struct {
		name            string
		params          events.RateLimitHitParams
		wantClientIPKey bool
	}{
		{
			name: "IP limit exceeded",
			params: events.RateLimitHitParams{
				LimitType:         "ip",
				Identifier:        "192.168.1.1",
				RequestsPerSecond: 10,
				Burst:             20,
				RetryAfterSeconds: 3,
				Path:              "/api/data",
				Method:            "GET",
				ClientIP:          "",
			},
			wantClientIPKey: false,
		},
		{
			name: "user limit exceeded",
			params: events.RateLimitHitParams{
				LimitType:         "user",
				Identifier:        "user-123",
				RequestsPerSecond: 100,
				Burst:             200,
				RetryAfterSeconds: 1,
				Path:              "/api/data",
				Method:            "GET",
				ClientIP:          "10.0.0.5",
			},
			wantClientIPKey: true,
		},
		{
			name: "user limit no client ip",
			params: events.RateLimitHitParams{
				LimitType:         "user",
				Identifier:        "user-456",
				RequestsPerSecond: 50,
				Burst:             100,
				RetryAfterSeconds: 2,
				Path:              "/upload",
				Method:            "POST",
				ClientIP:          "",
			},
			wantClientIPKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewRateLimitHit(tt.params)

			assertEvent(t, e, events.EventTypeRateLimitHit)
			requireSummaryContains(t, e.AISummary, tt.params.LimitType)
			requireSummaryContains(t, e.AISummary, tt.params.Identifier)
			requirePayloadString(t, e.Payload, "limit_type", tt.params.LimitType)
			requirePayloadString(t, e.Payload, "identifier", tt.params.Identifier)
			requirePayloadKey(t, e.Payload, "requests_per_second")
			requirePayloadKey(t, e.Payload, "burst")
			requirePayloadKey(t, e.Payload, "retry_after_seconds")
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)

			_, hasClientIP := e.Payload["client_ip"]
			if hasClientIP != tt.wantClientIPKey {
				t.Errorf("Payload has client_ip = %v, want %v", hasClientIP, tt.wantClientIPKey)
			}
		})
	}
}

// --- rate_limit.unidentified_client ---

func TestNewRateLimitUnidentified(t *testing.T) {
	tests := []struct {
		name   string
		params events.RateLimitUnidentifiedParams
	}{
		{
			name: "GET request",
			params: events.RateLimitUnidentifiedParams{
				Path:   "/api/resource",
				Method: "GET",
			},
		},
		{
			name: "POST request",
			params: events.RateLimitUnidentifiedParams{
				Path:   "/submit",
				Method: "POST",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewRateLimitUnidentified(tt.params)

			assertEvent(t, e, events.EventTypeRateLimitUnidentified)
			requireSummaryContains(t, e.AISummary, "IP")
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
		})
	}
}

// --- request.blocked ---

func TestNewRequestBlocked(t *testing.T) {
	tests := []struct {
		name   string
		params events.RequestBlockedParams
	}{
		{
			name: "blocked with client IP",
			params: events.RequestBlockedParams{
				Method:    "GET",
				Path:      "/admin",
				Reason:    "IP blocklist match",
				BlockedBy: "ip_blocklist",
				ClientIP:  "1.2.3.4",
			},
		},
		{
			name: "blocked without client IP",
			params: events.RequestBlockedParams{
				Method:    "POST",
				Path:      "/api/dangerous",
				Reason:    "security policy violation",
				BlockedBy: "security_headers",
				ClientIP:  "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewRequestBlocked(tt.params)

			assertEvent(t, e, events.EventTypeRequestBlocked)
			requireSummaryContains(t, e.AISummary, tt.params.BlockedBy)
			requireSummaryContains(t, e.AISummary, tt.params.Reason)
			requirePayloadString(t, e.Payload, "method", tt.params.Method)
			requirePayloadString(t, e.Payload, "path", tt.params.Path)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
			requirePayloadString(t, e.Payload, "blocked_by", tt.params.BlockedBy)
			requirePayloadString(t, e.Payload, "client_ip", tt.params.ClientIP)
		})
	}
}

// --- tls.certificate_issued ---

func TestNewTLSCertificateIssued(t *testing.T) {
	tests := []struct {
		name   string
		params events.TLSCertificateIssuedParams
	}{
		{
			name: "letsencrypt certificate",
			params: events.TLSCertificateIssuedParams{
				Domain:    "example.com",
				Provider:  "letsencrypt",
				ExpiresAt: "2026-06-26T00:00:00Z",
			},
		},
		{
			name: "self-signed certificate",
			params: events.TLSCertificateIssuedParams{
				Domain:    "localhost",
				Provider:  "self-signed",
				ExpiresAt: "2027-03-26T00:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewTLSCertificateIssued(tt.params)

			assertEvent(t, e, events.EventTypeTLSCertificateIssued)
			requireSummaryContains(t, e.AISummary, tt.params.Domain)
			requireSummaryContains(t, e.AISummary, tt.params.Provider)
			requirePayloadString(t, e.Payload, "domain", tt.params.Domain)
			requirePayloadString(t, e.Payload, "provider", tt.params.Provider)
			requirePayloadString(t, e.Payload, "expires_at", tt.params.ExpiresAt)
		})
	}
}

// --- user.created ---

func TestNewUserCreated(t *testing.T) {
	tests := []struct {
		name   string
		params events.UserCreatedParams
	}{
		{
			name: "standard user creation",
			params: events.UserCreatedParams{
				IdentityID: "id-newuser001",
				Email:      "newuser@example.com",
			},
		},
		{
			name: "admin user creation",
			params: events.UserCreatedParams{
				IdentityID: "id-admin002",
				Email:      "admin2@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewUserCreated(tt.params)

			assertEvent(t, e, events.EventTypeUserCreated)
			requireSummaryContains(t, e.AISummary, tt.params.Email)
			requireSummaryContains(t, e.AISummary, tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "identity_id", tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "email", tt.params.Email)
		})
	}
}

// --- user.deleted ---

func TestNewUserDeleted(t *testing.T) {
	tests := []struct {
		name   string
		params events.UserDeletedParams
	}{
		{
			name: "standard user deletion",
			params: events.UserDeletedParams{
				IdentityID: "id-olduser007",
				Email:      "leavinguser@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewUserDeleted(tt.params)

			assertEvent(t, e, events.EventTypeUserDeleted)
			requireSummaryContains(t, e.AISummary, tt.params.Email)
			requireSummaryContains(t, e.AISummary, tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "identity_id", tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "email", tt.params.Email)
		})
	}
}

// --- user.deactivated ---

func TestNewUserDeactivated(t *testing.T) {
	tests := []struct {
		name       string
		params     events.UserDeactivatedParams
		wantReason string
	}{
		{
			name: "deactivated with reason and actor",
			params: events.UserDeactivatedParams{
				IdentityID: "id-user001",
				Email:      "user@example.com",
				ActorID:    "admin-001",
				Reason:     "policy violation",
			},
			wantReason: "policy violation",
		},
		{
			name: "deactivated without actor or reason",
			params: events.UserDeactivatedParams{
				IdentityID: "id-user002",
				Email:      "other@example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewUserDeactivated(tt.params)

			assertEvent(t, e, events.EventTypeUserDeactivated)
			requireSummaryContains(t, e.AISummary, tt.params.Email)
			requireSummaryContains(t, e.AISummary, tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "identity_id", tt.params.IdentityID)
			requirePayloadString(t, e.Payload, "email", tt.params.Email)
			requirePayloadString(t, e.Payload, "actor_id", tt.params.ActorID)
			requirePayloadString(t, e.Payload, "reason", tt.params.Reason)
		})
	}
}

// TestTimestampIsRecent verifies that all constructors set the Timestamp to a
// time close to now (within 5 seconds), confirming it is not a zero value
// and not some fixed/hardcoded time.
func TestTimestampIsRecent(t *testing.T) {
	before := time.Now().UTC()

	constructors := []struct {
		name string
		fn   func() events.Event
	}{
		{"NewProxyStarted", func() events.Event {
			return events.NewProxyStarted(events.ProxyStartedParams{ListenAddr: ":8080", UpstreamAddr: "localhost:3000"})
		}},
		{"NewProxyKratosFlow", func() events.Event {
			return events.NewProxyKratosFlow(events.ProxyKratosFlowParams{Method: "GET", Path: "/self-service/login/browser"})
		}},
		{"NewAuthSuccess", func() events.Event {
			return events.NewAuthSuccess(events.AuthSuccessParams{Method: "GET", Path: "/", IdentityID: "id-1"})
		}},
		{"NewAuthFailed", func() events.Event {
			return events.NewAuthFailed(events.AuthFailedParams{Method: "GET", Path: "/", Reason: "test"})
		}},
		{"NewRateLimitHit", func() events.Event {
			return events.NewRateLimitHit(events.RateLimitHitParams{LimitType: "ip", Identifier: "1.1.1.1"})
		}},
		{"NewRateLimitUnidentified", func() events.Event {
			return events.NewRateLimitUnidentified(events.RateLimitUnidentifiedParams{Method: "GET", Path: "/"})
		}},
		{"NewRequestBlocked", func() events.Event {
			return events.NewRequestBlocked(events.RequestBlockedParams{Method: "GET", Path: "/", Reason: "test", BlockedBy: "test"})
		}},
		{"NewTLSCertificateIssued", func() events.Event {
			return events.NewTLSCertificateIssued(events.TLSCertificateIssuedParams{Domain: "example.com", Provider: "letsencrypt"})
		}},
		{"NewUserCreated", func() events.Event {
			return events.NewUserCreated(events.UserCreatedParams{IdentityID: "id-1", Email: "a@example.com"})
		}},
		{"NewUserDeleted", func() events.Event {
			return events.NewUserDeleted(events.UserDeletedParams{IdentityID: "id-1", Email: "a@example.com"})
		}},
		{"NewUserDeactivated", func() events.Event {
			return events.NewUserDeactivated(events.UserDeactivatedParams{IdentityID: "id-1", Email: "a@example.com"})
		}},
	}

	after := time.Now().UTC().Add(5 * time.Second)

	for _, tc := range constructors {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.fn()
			if e.Timestamp.Before(before) || e.Timestamp.After(after) {
				t.Errorf("%s: Timestamp %v is not within [%v, %v]", tc.name, e.Timestamp, before, after)
			}
		})
	}
}
