package events

import (
	"strings"
	"testing"
	"time"
)

func TestNewJWTValid(t *testing.T) {
	tests := []struct {
		name           string
		params         JWTValidParams
		wantEventType  string
		wantSummary    string
		wantPayloadKey string
	}{
		{
			name: "basic valid jwt event",
			params: JWTValidParams{
				Method:   "GET",
				Path:     "/api/users",
				Subject:  "user-123",
				Issuer:   "https://auth.example.com/",
				Audience: "my-api",
			},
			wantEventType:  EventTypeJWTValid,
			wantSummary:    "JWT validated",
			wantPayloadKey: "subject",
		},
		{
			name: "post request",
			params: JWTValidParams{
				Method:   "POST",
				Path:     "/api/orders",
				Subject:  "svc-account",
				Issuer:   "https://idp.example.com",
				Audience: "orders-api",
			},
			wantEventType:  EventTypeJWTValid,
			wantSummary:    "JWT validated",
			wantPayloadKey: "issuer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewJWTValid(tt.params)

			if ev.SchemaVersion != SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
			}
			if ev.EventType != tt.wantEventType {
				t.Errorf("EventType = %q, want %q", ev.EventType, tt.wantEventType)
			}
			if !strings.Contains(ev.AISummary, tt.wantSummary) {
				t.Errorf("AISummary = %q, want it to contain %q", ev.AISummary, tt.wantSummary)
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp should not be zero")
			}
			if _, ok := ev.Payload[tt.wantPayloadKey]; !ok {
				t.Errorf("Payload missing key %q", tt.wantPayloadKey)
			}
			if ev.Payload["method"] != tt.params.Method {
				t.Errorf("Payload[method] = %v, want %q", ev.Payload["method"], tt.params.Method)
			}
			if ev.Payload["path"] != tt.params.Path {
				t.Errorf("Payload[path] = %v, want %q", ev.Payload["path"], tt.params.Path)
			}
		})
	}
}

func TestNewJWTInvalid(t *testing.T) {
	tests := []struct {
		name   string
		params JWTInvalidParams
	}{
		{
			name: "invalid signature",
			params: JWTInvalidParams{
				Method: "GET",
				Path:   "/api/secure",
				Reason: "invalid_signature",
				Detail: "crypto/rsa: verification error",
			},
		},
		{
			name: "wrong issuer",
			params: JWTInvalidParams{
				Method: "POST",
				Path:   "/api/data",
				Reason: "invalid_issuer",
				Detail: "expected https://auth.example.com, got https://evil.com",
			},
		},
		{
			name: "no detail",
			params: JWTInvalidParams{
				Method: "GET",
				Path:   "/",
				Reason: "invalid_token",
				Detail: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewJWTInvalid(tt.params)

			if ev.EventType != EventTypeJWTInvalid {
				t.Errorf("EventType = %q, want %q", ev.EventType, EventTypeJWTInvalid)
			}
			if ev.SchemaVersion != SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, SchemaVersion)
			}
			if !strings.Contains(ev.AISummary, tt.params.Reason) {
				t.Errorf("AISummary = %q, want it to contain reason %q", ev.AISummary, tt.params.Reason)
			}
			if ev.Payload["reason"] != tt.params.Reason {
				t.Errorf("Payload[reason] = %v, want %q", ev.Payload["reason"], tt.params.Reason)
			}
			if ev.Payload["detail"] != tt.params.Detail {
				t.Errorf("Payload[detail] = %v, want %q", ev.Payload["detail"], tt.params.Detail)
			}
		})
	}
}

func TestNewJWTExpired(t *testing.T) {
	expiredAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
		params JWTExpiredParams
	}{
		{
			name: "expired token",
			params: JWTExpiredParams{
				Method:    "GET",
				Path:      "/api/data",
				Subject:   "user-456",
				ExpiredAt: expiredAt,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewJWTExpired(tt.params)

			if ev.EventType != EventTypeJWTExpired {
				t.Errorf("EventType = %q, want %q", ev.EventType, EventTypeJWTExpired)
			}
			if !strings.Contains(ev.AISummary, tt.params.Subject) {
				t.Errorf("AISummary = %q, want it to contain subject %q", ev.AISummary, tt.params.Subject)
			}
			if ev.Payload["subject"] != tt.params.Subject {
				t.Errorf("Payload[subject] = %v, want %q", ev.Payload["subject"], tt.params.Subject)
			}
			// expired_at should be present and parseable
			expiredAtStr, ok := ev.Payload["expired_at"].(string)
			if !ok {
				t.Fatalf("Payload[expired_at] is not a string: %T", ev.Payload["expired_at"])
			}
			parsed, err := time.Parse(time.RFC3339, expiredAtStr)
			if err != nil {
				t.Errorf("Payload[expired_at] = %q is not RFC3339: %v", expiredAtStr, err)
			}
			if !parsed.Equal(expiredAt) {
				t.Errorf("Payload[expired_at] = %v, want %v", parsed, expiredAt)
			}
		})
	}
}

func TestNewJWKSRefresh(t *testing.T) {
	tests := []struct {
		name   string
		params JWKSRefreshParams
	}{
		{
			name: "successful refresh",
			params: JWKSRefreshParams{
				JWKSURL:  "https://auth.example.com/.well-known/jwks.json",
				KeyCount: 3,
			},
		},
		{
			name: "single key",
			params: JWKSRefreshParams{
				JWKSURL:  "https://idp.example.com/jwks",
				KeyCount: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewJWKSRefresh(tt.params)

			if ev.EventType != EventTypeJWKSRefresh {
				t.Errorf("EventType = %q, want %q", ev.EventType, EventTypeJWKSRefresh)
			}
			if ev.Payload["jwks_url"] != tt.params.JWKSURL {
				t.Errorf("Payload[jwks_url] = %v, want %q", ev.Payload["jwks_url"], tt.params.JWKSURL)
			}
			if ev.Payload["key_count"] != tt.params.KeyCount {
				t.Errorf("Payload[key_count] = %v, want %d", ev.Payload["key_count"], tt.params.KeyCount)
			}
		})
	}
}

func TestNewJWKSError(t *testing.T) {
	tests := []struct {
		name   string
		params JWKSErrorParams
	}{
		{
			name: "network error",
			params: JWKSErrorParams{
				JWKSURL: "https://auth.example.com/.well-known/jwks.json",
				Detail:  "connection refused",
			},
		},
		{
			name: "parse error",
			params: JWKSErrorParams{
				JWKSURL: "https://auth.example.com/jwks",
				Detail:  "invalid JSON",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := NewJWKSError(tt.params)

			if ev.EventType != EventTypeJWKSError {
				t.Errorf("EventType = %q, want %q", ev.EventType, EventTypeJWKSError)
			}
			if ev.Payload["jwks_url"] != tt.params.JWKSURL {
				t.Errorf("Payload[jwks_url] = %v, want %q", ev.Payload["jwks_url"], tt.params.JWKSURL)
			}
			if ev.Payload["detail"] != tt.params.Detail {
				t.Errorf("Payload[detail] = %v, want %q", ev.Payload["detail"], tt.params.Detail)
			}
		})
	}
}
