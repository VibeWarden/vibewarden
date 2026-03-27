package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// TestIPFilterHandler_CaddyModule verifies the Caddy module metadata.
func TestIPFilterHandler_CaddyModule(t *testing.T) {
	info := IPFilterHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_ip_filter" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_ip_filter")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if mod == nil {
		t.Fatal("CaddyModule().New() returned nil")
	}
	if _, ok := mod.(*IPFilterHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *IPFilterHandler", mod)
	}
}

// TestIPFilterHandler_InterfaceGuards verifies the handler satisfies required Caddy interfaces.
func TestIPFilterHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*IPFilterHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*IPFilterHandler)(nil)
}

// TestIPFilterHandler_Provision_ValidAddresses verifies that Provision succeeds
// when all configured addresses are valid IPs or CIDRs.
func TestIPFilterHandler_Provision_ValidAddresses(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
	}{
		{"no addresses", []string{}},
		{"plain IPv4", []string{"192.168.1.1"}},
		{"IPv4 CIDR", []string{"10.0.0.0/8"}},
		{"plain IPv6", []string{"2001:db8::1"}},
		{"IPv6 CIDR", []string{"2001:db8::/32"}},
		{"mixed", []string{"10.0.0.0/8", "192.168.1.100", "2001:db8::/32"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &IPFilterHandler{
				Config: IPFilterHandlerConfig{
					Mode:      "blocklist",
					Addresses: tt.addresses,
				},
			}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Errorf("Provision() unexpected error: %v", err)
			}
		})
	}
}

// TestIPFilterHandler_Provision_InvalidAddress verifies that Provision returns
// an error when a configured address cannot be parsed.
func TestIPFilterHandler_Provision_InvalidAddress(t *testing.T) {
	h := &IPFilterHandler{
		Config: IPFilterHandlerConfig{
			Mode:      "blocklist",
			Addresses: []string{"not-an-ip"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err == nil {
		t.Error("Provision() expected error for invalid address, got nil")
	}
}

// TestIPFilterHandler_ServeHTTP_Blocklist tests blocklist mode behaviour.
func TestIPFilterHandler_ServeHTTP_Blocklist(t *testing.T) {
	tests := []struct {
		name           string
		addresses      []string
		remoteAddr     string
		wantNextCalled bool
		wantStatus     int
	}{
		{
			name:           "blocked IP in list",
			addresses:      []string{"192.168.1.100"},
			remoteAddr:     "192.168.1.100:54321",
			wantNextCalled: false,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "blocked IP in CIDR",
			addresses:      []string{"10.0.0.0/8"},
			remoteAddr:     "10.1.2.3:54321",
			wantNextCalled: false,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "allowed IP not in list",
			addresses:      []string{"192.168.1.100"},
			remoteAddr:     "203.0.113.5:54321",
			wantNextCalled: true,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "allowed IP not in CIDR",
			addresses:      []string{"10.0.0.0/8"},
			remoteAddr:     "203.0.113.5:54321",
			wantNextCalled: true,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "empty address list — all allowed",
			addresses:      []string{},
			remoteAddr:     "192.168.1.100:54321",
			wantNextCalled: true,
			wantStatus:     http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &IPFilterHandler{
				Config: IPFilterHandlerConfig{
					Mode:      "blocklist",
					Addresses: tt.addresses,
				},
			}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Fatalf("Provision() error: %v", err)
			}

			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			w := httptest.NewRecorder()

			if err := h.ServeHTTP(w, req, next); err != nil {
				t.Errorf("ServeHTTP() unexpected error: %v", err)
			}

			if nextCalled != tt.wantNextCalled {
				t.Errorf("nextCalled = %v, want %v", nextCalled, tt.wantNextCalled)
			}
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestIPFilterHandler_ServeHTTP_Allowlist tests allowlist mode behaviour.
func TestIPFilterHandler_ServeHTTP_Allowlist(t *testing.T) {
	tests := []struct {
		name           string
		addresses      []string
		remoteAddr     string
		wantNextCalled bool
		wantStatus     int
	}{
		{
			name:           "allowed IP in list",
			addresses:      []string{"203.0.113.5"},
			remoteAddr:     "203.0.113.5:54321",
			wantNextCalled: true,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "allowed IP in CIDR",
			addresses:      []string{"203.0.113.0/24"},
			remoteAddr:     "203.0.113.42:54321",
			wantNextCalled: true,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "blocked IP not in list",
			addresses:      []string{"203.0.113.5"},
			remoteAddr:     "192.168.1.100:54321",
			wantNextCalled: false,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "blocked IP not in CIDR",
			addresses:      []string{"203.0.113.0/24"},
			remoteAddr:     "10.0.0.1:54321",
			wantNextCalled: false,
			wantStatus:     http.StatusForbidden,
		},
		{
			name:           "empty allowlist — all blocked",
			addresses:      []string{},
			remoteAddr:     "203.0.113.5:54321",
			wantNextCalled: false,
			wantStatus:     http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &IPFilterHandler{
				Config: IPFilterHandlerConfig{
					Mode:      "allowlist",
					Addresses: tt.addresses,
				},
			}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Fatalf("Provision() error: %v", err)
			}

			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			w := httptest.NewRecorder()

			if err := h.ServeHTTP(w, req, next); err != nil {
				t.Errorf("ServeHTTP() unexpected error: %v", err)
			}

			if nextCalled != tt.wantNextCalled {
				t.Errorf("nextCalled = %v, want %v", nextCalled, tt.wantNextCalled)
			}
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestIPFilterHandler_MatchesAny tests the IP matching logic directly.
func TestIPFilterHandler_MatchesAny(t *testing.T) {
	tests := []struct {
		name      string
		addresses []string
		clientIP  string
		want      bool
	}{
		{"exact match", []string{"192.168.1.100"}, "192.168.1.100", true},
		{"no match", []string{"192.168.1.100"}, "192.168.1.101", false},
		{"CIDR match", []string{"10.0.0.0/8"}, "10.255.255.254", true},
		{"CIDR no match", []string{"10.0.0.0/8"}, "11.0.0.1", false},
		{"empty list", []string{}, "10.0.0.1", false},
		{"IPv6 exact", []string{"2001:db8::1"}, "[2001:db8::1]", true},
		{"IPv6 CIDR match", []string{"2001:db8::/32"}, "[2001:db8::cafe]", true},
		{"IPv6 CIDR no match", []string{"2001:db8::/32"}, "[2001:db9::1]", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &IPFilterHandler{
				Config: IPFilterHandlerConfig{
					Mode:      "blocklist",
					Addresses: tt.addresses,
				},
			}
			if err := h.Provision(gocaddy.Context{}); err != nil {
				t.Fatalf("Provision() error: %v", err)
			}

			// Build a request with the given client IP and let ServeHTTP decide.
			// We verify indirectly via the response code in the blocklist scenario:
			// matched => 403, not matched => 200 (next called).
			nextCalled := false
			next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				return nil
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			// IPv6 addresses in RemoteAddr must be bracketed: [addr]:port.
			req.RemoteAddr = tt.clientIP + ":12345"
			w := httptest.NewRecorder()
			_ = h.ServeHTTP(w, req, next)

			// In blocklist mode: matched == blocked == !nextCalled.
			if tt.want && nextCalled {
				t.Errorf("IP %q matched list but next was called (expected block in blocklist mode)", tt.clientIP)
			}
			if !tt.want && !nextCalled {
				t.Errorf("IP %q did not match list but next was not called (expected allow in blocklist mode)", tt.clientIP)
			}
		})
	}
}

// TestIPFilterHandler_UnknownMode_TreatedAsBlocklist verifies that an
// unrecognised mode value falls back to blocklist semantics.
func TestIPFilterHandler_UnknownMode_TreatedAsBlocklist(t *testing.T) {
	h := &IPFilterHandler{
		Config: IPFilterHandlerConfig{
			Mode:      "unknown",
			Addresses: []string{"10.0.0.1"},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	nextCalled := false
	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		nextCalled = true
		return nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:54321" // in list — should be blocked
	w := httptest.NewRecorder()

	_ = h.ServeHTTP(w, req, next)

	if nextCalled {
		t.Error("expected request to be blocked in unknown mode (blocklist fallback), but next was called")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}
