package middleware_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	domainwebhook "github.com/vibewarden/vibewarden/internal/domain/webhook"
	"github.com/vibewarden/vibewarden/internal/middleware"
)

// fakeEventLogger captures emitted events for assertions.
type fakeEventLogger struct {
	logged []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.logged = append(f.logged, ev)
	return nil
}

func hmacSHA256Hex(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message)) //nolint:errcheck
	return hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookSignatureMiddleware(t *testing.T) {
	const secret = "test_secret"
	const path = "/hooks/github"
	const body = `{"action":"opened"}`

	validSig := "sha256=" + hmacSHA256Hex(secret, body)

	rules := []middleware.WebhookSignatureRule{
		{
			Path: path,
			Config: domainwebhook.VerifyConfig{
				Provider: domainwebhook.ProviderGitHub,
				Secret:   secret,
			},
		},
	}

	tests := []struct {
		name          string
		path          string
		body          string
		sigHeader     string
		wantStatus    int
		wantEventType string
	}{
		{
			name:          "valid signature — 200 and passes through",
			path:          path,
			body:          body,
			sigHeader:     validSig,
			wantStatus:    http.StatusOK,
			wantEventType: events.EventTypeWebhookSignatureValid,
		},
		{
			name:          "invalid signature — 401",
			path:          path,
			body:          body,
			sigHeader:     "sha256=deadbeef",
			wantStatus:    http.StatusUnauthorized,
			wantEventType: events.EventTypeWebhookSignatureInvalid,
		},
		{
			name:          "missing signature — 401",
			path:          path,
			body:          body,
			sigHeader:     "",
			wantStatus:    http.StatusUnauthorized,
			wantEventType: events.EventTypeWebhookSignatureInvalid,
		},
		{
			name:       "path not configured — passes through without event",
			path:       "/other/path",
			body:       body,
			sigHeader:  "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &fakeEventLogger{}

			handler := middleware.WebhookSignatureMiddleware(rules, logger)(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			if tt.sigHeader != "" {
				req.Header.Set("X-Hub-Signature-256", tt.sigHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantEventType != "" {
				if len(logger.logged) == 0 {
					t.Fatalf("no events logged, want event type %q", tt.wantEventType)
				}
				if logger.logged[0].EventType != tt.wantEventType {
					t.Errorf("event type = %q, want %q", logger.logged[0].EventType, tt.wantEventType)
				}
			} else if len(logger.logged) > 0 {
				t.Errorf("unexpected event logged: %v", logger.logged[0].EventType)
			}
		})
	}
}

// TestWebhookSignatureMiddlewareBodyRestored verifies that the request body is
// still readable by the downstream handler after signature verification.
func TestWebhookSignatureMiddlewareBodyRestored(t *testing.T) {
	const secret = "restore_secret"
	const path = "/hooks/test"
	const body = `{"restore":"me"}`

	validSig := "sha256=" + hmacSHA256Hex(secret, body)

	rules := []middleware.WebhookSignatureRule{
		{
			Path: path,
			Config: domainwebhook.VerifyConfig{
				Provider: domainwebhook.ProviderGitHub,
				Secret:   secret,
			},
		},
	}

	var gotBody []byte
	handler := middleware.WebhookSignatureMiddleware(rules, nil)(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			buf := new(bytes.Buffer)
			buf.ReadFrom(r.Body) //nolint:errcheck
			gotBody = buf.Bytes()
		}),
	)

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", validSig)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if string(gotBody) != body {
		t.Errorf("downstream body = %q, want %q", gotBody, body)
	}
}

// TestWebhookSignatureMiddlewareNilEventLogger verifies that the middleware
// does not panic when eventLogger is nil.
func TestWebhookSignatureMiddlewareNilEventLogger(t *testing.T) {
	const secret = "nil_logger_secret"
	const path = "/hooks/nil"

	rules := []middleware.WebhookSignatureRule{
		{
			Path: path,
			Config: domainwebhook.VerifyConfig{
				Provider: domainwebhook.ProviderGitHub,
				Secret:   secret,
			},
		},
	}

	handler := middleware.WebhookSignatureMiddleware(rules, nil)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	rec := httptest.NewRecorder()

	// Should not panic — missing sig returns 401.
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
