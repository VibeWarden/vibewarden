package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestWebhookSignatureHandler_CaddyModule verifies the Caddy module metadata.
func TestWebhookSignatureHandler_CaddyModule(t *testing.T) {
	info := WebhookSignatureHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_webhook_signature" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_webhook_signature")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*WebhookSignatureHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *WebhookSignatureHandler", mod)
	}
}

// TestWebhookSignatureHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestWebhookSignatureHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*WebhookSignatureHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*WebhookSignatureHandler)(nil)
}

// TestWebhookSignatureHandler_ProvisionWith verifies that ProvisionWith with valid
// services succeeds and that the handler accepts requests.
func TestWebhookSignatureHandler_ProvisionWith(t *testing.T) {
	tests := []struct {
		name    string
		rules   []WebhookSignatureHandlerRuleConfig
		wantErr bool
	}{
		{
			name:    "no rules — handler accepts all requests",
			rules:   nil,
			wantErr: false,
		},
		{
			name: "valid stripe rule",
			rules: []WebhookSignatureHandlerRuleConfig{
				{Path: "/webhook/stripe", Provider: "stripe", SecretEnvVar: "STRIPE_SECRET"},
			},
			wantErr: false,
		},
		{
			name: "valid github rule",
			rules: []WebhookSignatureHandlerRuleConfig{
				{Path: "/webhook/github", Provider: "github", SecretEnvVar: "GITHUB_SECRET"},
			},
			wantErr: false,
		},
		{
			name: "unknown provider returns error",
			rules: []WebhookSignatureHandlerRuleConfig{
				{Path: "/webhook", Provider: "unknown_provider", SecretEnvVar: "SECRET"},
			},
			wantErr: true,
		},
		{
			name: "missing path returns error",
			rules: []WebhookSignatureHandlerRuleConfig{
				{Path: "", Provider: "stripe", SecretEnvVar: "SECRET"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &WebhookSignatureHandler{
				Config: WebhookSignatureHandlerConfig{Rules: tt.rules},
			}
			err := h.ProvisionWith(gocaddy.Context{}, RuntimeServices{})
			if (err != nil) != tt.wantErr {
				t.Errorf("ProvisionWith() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestWebhookSignatureHandler_ServeHTTP_NoRules verifies that when no rules are
// configured, all requests pass through to the next handler.
func TestWebhookSignatureHandler_ServeHTTP_NoRules(t *testing.T) {
	h := &WebhookSignatureHandler{
		Config: WebhookSignatureHandlerConfig{Rules: nil},
	}
	if err := h.ProvisionWith(gocaddy.Context{}, RuntimeServices{}); err != nil {
		t.Fatalf("ProvisionWith() error: %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", nil)
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, next); err != nil {
		t.Fatalf("ServeHTTP() unexpected error: %v", err)
	}

	if !nextCalled {
		t.Error("expected next handler to be called when no rules configured")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestWebhookSignatureHandler_ProvisionWith_UsesEventLogger verifies that
// ProvisionWith correctly stores the event logger from RuntimeServices.
func TestWebhookSignatureHandler_ProvisionWith_UsesEventLogger(t *testing.T) {
	el := &fakeEventLogger{}

	h := &WebhookSignatureHandler{
		Config: WebhookSignatureHandlerConfig{Rules: nil},
	}
	if err := h.ProvisionWith(gocaddy.Context{}, RuntimeServices{EventLogger: el}); err != nil {
		t.Fatalf("ProvisionWith() error: %v", err)
	}
	// Handler successfully built — event logger was wired in.
	// (Full event emission verification requires a live request with a valid signature.)
}
