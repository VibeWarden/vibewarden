package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/resilience"
	"github.com/vibewarden/vibewarden/internal/domain/waf"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Test fakes
// ---------------------------------------------------------------------------

// wafDetectionCall records a single IncWAFDetection call.
type wafDetectionCall struct {
	rule string
	mode string
}

// fakeWAFCollector implements ports.MetricsCollector for WAF middleware tests.
// It records WAF detection increments; all other methods are no-ops.
type fakeWAFCollector struct {
	detections []wafDetectionCall
}

var _ ports.MetricsCollector = (*fakeWAFCollector)(nil)

func (f *fakeWAFCollector) IncRequestTotal(_, _, _ string)                               {}
func (f *fakeWAFCollector) ObserveRequestDuration(_, _ string, _ time.Duration)          {}
func (f *fakeWAFCollector) IncRateLimitHit(_ string)                                     {}
func (f *fakeWAFCollector) IncAuthDecision(_ string)                                     {}
func (f *fakeWAFCollector) IncUpstreamError()                                            {}
func (f *fakeWAFCollector) IncUpstreamTimeout()                                          {}
func (f *fakeWAFCollector) IncUpstreamRetry(_ string)                                    {}
func (f *fakeWAFCollector) SetActiveConnections(_ int)                                   {}
func (f *fakeWAFCollector) SetCircuitBreakerState(_ context.Context, _ resilience.State) {}
func (f *fakeWAFCollector) IncWAFDetection(rule, mode string) {
	f.detections = append(f.detections, wafDetectionCall{rule: rule, mode: mode})
}
func (f *fakeWAFCollector) IncEgressRequestTotal(_, _, _ string)               {}
func (f *fakeWAFCollector) ObserveEgressDuration(_, _ string, _ time.Duration) {}
func (f *fakeWAFCollector) IncEgressErrorTotal(_ string)                       {}
func (f *fakeWAFCollector) SetTLSCertExpirySeconds(_ string, _ float64)        {}

// fakeWAFAuditLogger records audit events.
type fakeWAFAuditLogger struct {
	events []audit.AuditEvent
}

var _ ports.AuditEventLogger = (*fakeWAFAuditLogger)(nil)

func (f *fakeWAFAuditLogger) Log(_ context.Context, ev audit.AuditEvent) error {
	f.events = append(f.events, ev)
	return nil
}

func (f *fakeWAFAuditLogger) hasEventType(et audit.EventType) bool {
	for _, e := range f.events {
		if e.EventType == et {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func defaultRuleSet(t *testing.T) waf.RuleSet {
	t.Helper()
	rs, err := waf.NewRuleSet(waf.BuiltinRules())
	if err != nil {
		t.Fatalf("NewRuleSet: %v", err)
	}
	return rs
}

func defaultWAFCfg() WAFConfig {
	return WAFConfig{
		Mode:              WAFModeBlock,
		EnabledCategories: nil, // all enabled
		ExemptPaths:       nil,
	}
}

func buildWAFHandler(t *testing.T, cfg WAFConfig, mc *fakeWAFCollector, al *fakeWAFAuditLogger) http.Handler {
	t.Helper()
	rs := defaultRuleSet(t)
	mw := WAFMiddleware(rs, cfg, newTestLogger(), mc, al)
	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

// ---------------------------------------------------------------------------
// Block mode tests
// ---------------------------------------------------------------------------

func TestWAFMiddleware_BlockMode_CleanRequest_Passes(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	req := httptest.NewRequest(http.MethodGet, "/api/products?q=laptop", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if len(mc.detections) != 0 {
		t.Errorf("expected 0 detections, got %d", len(mc.detections))
	}
	if len(al.events) != 0 {
		t.Errorf("expected 0 audit events, got %d", len(al.events))
	}
}

func TestWAFMiddleware_BlockMode_SQLi_Blocked(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	// Classic UNION SELECT payload in a query parameter.
	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	if errResp.Error != "waf_blocked" {
		t.Errorf("error code = %q, want \"waf_blocked\"", errResp.Error)
	}
	if !strings.Contains(errResp.Message, "sqli-union-select") {
		t.Errorf("message %q should contain rule name %q", errResp.Message, "sqli-union-select")
	}

	if len(mc.detections) == 0 {
		t.Fatal("expected at least 1 detection metric")
	}
	if mc.detections[0].mode != "block" {
		t.Errorf("metric mode = %q, want \"block\"", mc.detections[0].mode)
	}

	if !al.hasEventType(audit.EventTypeWAFBlocked) {
		t.Error("expected audit.waf.blocked event")
	}
}

func TestWAFMiddleware_BlockMode_XSS_Blocked(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	req := httptest.NewRequest(http.MethodGet, "/page?title=<script>alert(1)</script>", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestWAFMiddleware_BlockMode_PathTraversal_Blocked(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	req := httptest.NewRequest(http.MethodGet, "/files?path=../../etc/passwd", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestWAFMiddleware_BlockMode_CommandInjection_Blocked(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	// Semicolon must be percent-encoded (%3B) because Go's url.ParseQuery
	// treats a raw semicolon as a query-parameter separator (deprecated but
	// still honored), which would split the query incorrectly.
	req := httptest.NewRequest(http.MethodGet, "/run?cmd=%3B+cat+/etc/passwd", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestWAFMiddleware_BlockMode_Body_SQLi_Blocked(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	body := strings.NewReader(`{"query":"' OR 1=1--"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/query", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Detect mode tests
// ---------------------------------------------------------------------------

func TestWAFMiddleware_DetectMode_Attack_Passes_Through(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	cfg := WAFConfig{Mode: WAFModeDetect}
	h := buildWAFHandler(t, cfg, mc, al)

	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("detect mode: status = %d, want 200 (request should pass through)", rr.Code)
	}
	if len(mc.detections) == 0 {
		t.Fatal("expected at least 1 detection metric in detect mode")
	}
	if mc.detections[0].mode != "detect" {
		t.Errorf("metric mode = %q, want \"detect\"", mc.detections[0].mode)
	}

	if !al.hasEventType(audit.EventTypeWAFDetection) {
		t.Error("expected audit.waf.detection event in detect mode")
	}
	if al.hasEventType(audit.EventTypeWAFBlocked) {
		t.Error("must NOT emit audit.waf.blocked in detect mode")
	}
}

func TestWAFMiddleware_DetectMode_XSS_Passes_Through(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	cfg := WAFConfig{Mode: WAFModeDetect}
	h := buildWAFHandler(t, cfg, mc, al)

	req := httptest.NewRequest(http.MethodGet, "/page?title=<script>alert(1)</script>", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("detect mode: status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Exempt paths
// ---------------------------------------------------------------------------

func TestWAFMiddleware_ExemptPaths_SkipScanning(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		query       string
		exemptPaths []string
		wantStatus  int
	}{
		{
			name:        "vibewarden prefix always exempt",
			path:        "/_vibewarden/health",
			query:       "?q=1+UNION+SELECT+1,2,3",
			exemptPaths: nil,
			wantStatus:  http.StatusOK,
		},
		{
			name:        "custom exempt path",
			path:        "/internal/debug",
			query:       "?q=1+UNION+SELECT+1,2,3",
			exemptPaths: []string{"/internal/*"},
			wantStatus:  http.StatusOK,
		},
		{
			name:        "non-exempt path with attack is blocked",
			path:        "/api/search",
			query:       "?q=1+UNION+SELECT+1,2,3",
			exemptPaths: []string{"/internal/*"},
			wantStatus:  http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := &fakeWAFCollector{}
			al := &fakeWAFAuditLogger{}
			cfg := WAFConfig{
				Mode:        WAFModeBlock,
				ExemptPaths: tt.exemptPaths,
			}
			h := buildWAFHandler(t, cfg, mc, al)

			req := httptest.NewRequest(http.MethodGet, tt.path+tt.query, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Per-category toggles
// ---------------------------------------------------------------------------

func TestWAFMiddleware_DisabledCategory_Passes(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	cfg := WAFConfig{
		Mode: WAFModeBlock,
		EnabledCategories: map[waf.Category]bool{
			waf.CategorySQLInjection: false, // disable SQLi — attack should pass through
		},
	}
	h := buildWAFHandler(t, cfg, mc, al)

	// This would normally be blocked (sqli-union-select).
	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("disabled category: status = %d, want 200", rr.Code)
	}
}

func TestWAFMiddleware_OtherCategoriesStillBlock_WhenOneCategoryDisabled(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	cfg := WAFConfig{
		Mode: WAFModeBlock,
		EnabledCategories: map[waf.Category]bool{
			waf.CategorySQLInjection: false, // disable SQLi only
		},
	}
	h := buildWAFHandler(t, cfg, mc, al)

	// XSS is still enabled — should be blocked.
	req := httptest.NewRequest(http.MethodGet, "/page?title=<script>alert(1)</script>", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("enabled category XSS: status = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Nil optional dependencies
// ---------------------------------------------------------------------------

func TestWAFMiddleware_NilCollectorAndAuditLogger_NoPanic(t *testing.T) {
	rs := defaultRuleSet(t)
	mw := WAFMiddleware(rs, defaultWAFCfg(), newTestLogger(), nil, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Clean request — no detections.
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}

	// Attack request — should block without panicking (nil mc and auditLogger).
	req2 := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Errorf("attack status = %d, want 403", rr2.Code)
	}
}

// ---------------------------------------------------------------------------
// Invalid mode falls back to detect (pass-through)
// ---------------------------------------------------------------------------

func TestWAFMiddleware_InvalidMode_FallsBackToDetect(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	cfg := WAFConfig{Mode: WAFMode("invalid")}
	h := buildWAFHandler(t, cfg, mc, al)

	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("invalid mode should fall back to detect (pass-through): status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Header injection attack
// ---------------------------------------------------------------------------

func TestWAFMiddleware_Header_Attack_Blocked(t *testing.T) {
	mc := &fakeWAFCollector{}
	al := &fakeWAFAuditLogger{}
	h := buildWAFHandler(t, defaultWAFCfg(), mc, al)

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	// The WAF scans the User-Agent header.
	req.Header.Set("User-Agent", "<script>alert(1)</script>")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("header attack: status = %d, want 403", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Body read failures
// ---------------------------------------------------------------------------

// failingBody is a request body whose first Read returns err.
type failingBody struct{ err error }

func (b failingBody) Read([]byte) (int, error) { return 0, b.err }
func (b failingBody) Close() error             { return nil }

// TestWAFMiddleware_BodyReadTimeout_Returns408 verifies that a body read cut
// by the server's read deadline (the client stalled mid-upload) is answered
// with 408 instead of being forwarded with a dead body.
func TestWAFMiddleware_BodyReadTimeout_Returns408(t *testing.T) {
	for name, err := range map[string]error{
		"http/1.1 net.OpError":        &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded},
		"http/2 wrapped deadline":     errors.Join(errors.New("http2: stream read"), os.ErrDeadlineExceeded),
		"bare os.ErrDeadlineExceeded": os.ErrDeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			h := buildWAFHandler(t, defaultWAFCfg(), &fakeWAFCollector{}, &fakeWAFAuditLogger{})
			req := httptest.NewRequest(http.MethodPost, "/upload", nil)
			req.Body = io.NopCloser(failingBody{err: err})
			req.ContentLength = 1000
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusRequestTimeout {
				t.Fatalf("status = %d, want 408; body %q", rec.Code, rec.Body.String())
			}
			var body ErrorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v (%q)", err, rec.Body.String())
			}
			if body.Error != "request_timeout" {
				t.Errorf("error code = %q, want request_timeout", body.Error)
			}
		})
	}
}

// TestWAFMiddleware_OtherBodyError_FailsOpen verifies that a body read error
// that is not a timeout keeps the documented fail-open behaviour.
func TestWAFMiddleware_OtherBodyError_FailsOpen(t *testing.T) {
	h := buildWAFHandler(t, defaultWAFCfg(), &fakeWAFCollector{}, &fakeWAFAuditLogger{})
	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.Body = io.NopCloser(failingBody{err: errors.New("connection reset by peer")})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (fail open)", rec.Code)
	}
}
