package caddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"

	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeHealthChecker implements ports.UpstreamHealthChecker for handler tests.
type fakeHealthChecker struct {
	status    domainheal.UpstreamStatus
	lastError string
}

func (f *fakeHealthChecker) Start(_ context.Context) error { return nil }
func (f *fakeHealthChecker) Stop(_ context.Context) error  { return nil }
func (f *fakeHealthChecker) CurrentStatus() domainheal.UpstreamStatus {
	return f.status
}
func (f *fakeHealthChecker) Snapshot() ports.UpstreamHealthSnapshot {
	return ports.UpstreamHealthSnapshot{
		Status:    f.status.String(),
		LastError: f.lastError,
	}
}

var _ ports.UpstreamHealthChecker = (*fakeHealthChecker)(nil)

func TestHealthHandler_ProvisionWith(t *testing.T) {
	checker := &fakeHealthChecker{status: domainheal.StatusHealthy}
	svc := RuntimeServices{
		UpstreamHealthChecker: checker,
		SidecarVersion:        "v0.18.2",
	}

	h := &HealthHandler{}
	if err := h.ProvisionWith(gocaddy.Context{}, svc); err != nil {
		t.Fatalf("ProvisionWith() error = %v", err)
	}

	if h.checker != checker {
		t.Error("checker not set from RuntimeServices")
	}
	if h.version != "v0.18.2" {
		t.Errorf("version = %q, want %q", h.version, "v0.18.2")
	}
}

func TestHealthHandler_ProvisionWith_SiteName(t *testing.T) {
	h := &HealthHandler{
		Config: HealthHandlerConfig{SiteName: "demo"},
	}
	svc := RuntimeServices{SidecarVersion: "v1.0.0"}

	if err := h.ProvisionWith(gocaddy.Context{}, svc); err != nil {
		t.Fatalf("ProvisionWith() error = %v", err)
	}
	if h.siteName != "demo" {
		t.Errorf("siteName = %q, want %q", h.siteName, "demo")
	}
}

func serveHealth(t *testing.T, h *HealthHandler) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
	w := httptest.NewRecorder()

	if err := h.ServeHTTP(w, req, nil); err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("HTTP status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return out
}

func TestHealthHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		checker        *fakeHealthChecker
		siteName       string
		version        string
		wantStatus     string
		wantUpstream   string
		wantSiteField  bool
		wantSiteValue  string
		wantVersionStr string
	}{
		{
			name:         "no checker → unknown upstream, degraded outer",
			checker:      nil,
			wantStatus:   "degraded",
			wantUpstream: "unknown",
		},
		{
			name:         "checker healthy → ok upstream, ok outer",
			checker:      &fakeHealthChecker{status: domainheal.StatusHealthy},
			wantStatus:   "ok",
			wantUpstream: "ok",
		},
		{
			name:         "checker unhealthy → failing upstream, degraded outer",
			checker:      &fakeHealthChecker{status: domainheal.StatusUnhealthy, lastError: "timeout"},
			wantStatus:   "degraded",
			wantUpstream: "failing",
		},
		{
			name:         "checker unknown → unknown upstream, degraded outer",
			checker:      &fakeHealthChecker{status: domainheal.StatusUnknown},
			wantStatus:   "degraded",
			wantUpstream: "unknown",
		},
		{
			name:          "site name rendered in response",
			checker:       &fakeHealthChecker{status: domainheal.StatusHealthy},
			siteName:      "demo",
			wantStatus:    "ok",
			wantUpstream:  "ok",
			wantSiteField: true,
			wantSiteValue: "demo",
		},
		{
			name:           "version rendered in response",
			checker:        &fakeHealthChecker{status: domainheal.StatusHealthy},
			version:        "v0.18.2",
			wantStatus:     "ok",
			wantUpstream:   "ok",
			wantVersionStr: "v0.18.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &HealthHandler{
				Config:   HealthHandlerConfig{SiteName: tt.siteName},
				version:  tt.version,
				siteName: tt.siteName,
			}
			if tt.checker != nil {
				h.checker = tt.checker
			}

			out := serveHealth(t, h)

			if got, _ := out["status"].(string); got != tt.wantStatus {
				t.Errorf("status = %q, want %q", got, tt.wantStatus)
			}

			comps, _ := out["components"].(map[string]any)
			if comps == nil {
				t.Fatal("components field missing")
			}
			if got, _ := comps["upstream"].(string); got != tt.wantUpstream {
				t.Errorf("components.upstream = %q, want %q", got, tt.wantUpstream)
			}
			if got, _ := comps["sidecar"].(string); got != "ok" {
				t.Errorf("components.sidecar = %q, want %q", got, "ok")
			}

			if tt.wantSiteField {
				if got, _ := out["site"].(string); got != tt.wantSiteValue {
					t.Errorf("site = %q, want %q", got, tt.wantSiteValue)
				}
			}
			if tt.wantVersionStr != "" {
				if got, _ := out["version"].(string); got != tt.wantVersionStr {
					t.Errorf("version = %q, want %q", got, tt.wantVersionStr)
				}
			}
		})
	}
}

func TestHealthHandler_AlwaysHTTP200(t *testing.T) {
	// All upstream states must return 200 — sidecar is up; outer status is
	// informational. 503 is reserved for /_vibewarden/ready.
	statuses := []domainheal.UpstreamStatus{
		domainheal.StatusUnknown,
		domainheal.StatusHealthy,
		domainheal.StatusUnhealthy,
	}
	for _, s := range statuses {
		h := &HealthHandler{checker: &fakeHealthChecker{status: s}}
		req := httptest.NewRequest(http.MethodGet, "/_vibewarden/health", nil)
		w := httptest.NewRecorder()
		_ = h.ServeHTTP(w, req, nil)
		if w.Code != http.StatusOK {
			t.Errorf("upstream status %v → HTTP %d, want 200", s, w.Code)
		}
	}
}

func TestMapUpstreamStatus(t *testing.T) {
	tests := []struct {
		name      string
		status    domainheal.UpstreamStatus
		lastError string
		wantStr   string
		wantHlt   bool
	}{
		{"healthy → ok", domainheal.StatusHealthy, "", "ok", true},
		{"unhealthy → failing", domainheal.StatusUnhealthy, "err", "failing", false},
		{"unknown → unknown", domainheal.StatusUnknown, "", "unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mapUpstreamStatus(tt.status, tt.lastError)
			if s.String() != tt.wantStr {
				t.Errorf("String() = %q, want %q", s.String(), tt.wantStr)
			}
			if s.Healthy() != tt.wantHlt {
				t.Errorf("Healthy() = %v, want %v", s.Healthy(), tt.wantHlt)
			}
		})
	}
}
