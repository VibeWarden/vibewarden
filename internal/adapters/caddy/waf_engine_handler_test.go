package caddy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	gocaddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"

	domainwaf "github.com/vibewarden/vibewarden/internal/domain/waf"
)

// ---------------------------------------------------------------------------
// CaddyModule metadata
// ---------------------------------------------------------------------------

func TestWAFEngineHandler_CaddyModule(t *testing.T) {
	info := WAFEngineHandler{}.CaddyModule()

	if info.ID != "http.handlers.vibewarden_waf_engine" {
		t.Errorf("CaddyModule().ID = %q, want %q", info.ID, "http.handlers.vibewarden_waf_engine")
	}
	if info.New == nil {
		t.Fatal("CaddyModule().New is nil")
	}
	mod := info.New()
	if _, ok := mod.(*WAFEngineHandler); !ok {
		t.Errorf("CaddyModule().New() returned %T, want *WAFEngineHandler", mod)
	}
}

// ---------------------------------------------------------------------------
// Interface guards
// ---------------------------------------------------------------------------

func TestWAFEngineHandler_InterfaceGuards(t *testing.T) {
	var _ gocaddy.Provisioner = (*WAFEngineHandler)(nil)
	var _ caddyhttp.MiddlewareHandler = (*WAFEngineHandler)(nil)
}

// ---------------------------------------------------------------------------
// buildEnabledRules
// ---------------------------------------------------------------------------

func TestBuildEnabledRules_AllEnabled(t *testing.T) {
	cfg := WAFEngineHandlerRulesConfig{
		SQLInjection:  true,
		XSS:           true,
		PathTraversal: true,
		CmdInjection:  true,
	}
	rules, err := buildEnabledRules(cfg)
	if err != nil {
		t.Fatalf("buildEnabledRules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("expected at least one rule when all categories enabled")
	}
}

func TestBuildEnabledRules_AllDisabled_ReturnsAllRules(t *testing.T) {
	// When all categories are disabled the function must still return a
	// non-empty slice so the domain RuleSet invariant is satisfied.
	cfg := WAFEngineHandlerRulesConfig{}
	rules, err := buildEnabledRules(cfg)
	if err != nil {
		t.Fatalf("buildEnabledRules: %v", err)
	}
	if len(rules) == 0 {
		t.Error("expected fallback to all rules when all categories disabled")
	}
}

func TestBuildEnabledRules_SubsetEnabled(t *testing.T) {
	sqlOnly := WAFEngineHandlerRulesConfig{SQLInjection: true}
	rules, err := buildEnabledRules(sqlOnly)
	if err != nil {
		t.Fatalf("buildEnabledRules: %v", err)
	}
	for _, r := range rules {
		if r.Category() != domainwaf.CategorySQLInjection {
			t.Errorf("unexpected category %q in SQLi-only rule set", r.Category())
		}
	}
}

// ---------------------------------------------------------------------------
// buildEnabledCategories
// ---------------------------------------------------------------------------

func TestBuildEnabledCategories_MapsCorrectly(t *testing.T) {
	cfg := WAFEngineHandlerRulesConfig{
		SQLInjection:  true,
		XSS:           false,
		PathTraversal: true,
		CmdInjection:  false,
	}
	cats := buildEnabledCategories(cfg)

	if !cats[domainwaf.CategorySQLInjection] {
		t.Error("sqli should be enabled")
	}
	if cats[domainwaf.CategoryXSS] {
		t.Error("xss should be disabled")
	}
	if !cats[domainwaf.CategoryPathTraversal] {
		t.Error("path_traversal should be enabled")
	}
	if cats[domainwaf.CategoryCommandInjection] {
		t.Error("cmd_injection should be disabled")
	}
}

// ---------------------------------------------------------------------------
// Provision
// ---------------------------------------------------------------------------

func TestWAFEngineHandler_Provision_Succeeds(t *testing.T) {
	h := &WAFEngineHandler{
		Config: WAFEngineHandlerConfig{
			Mode: "block",
			Rules: WAFEngineHandlerRulesConfig{
				SQLInjection:  true,
				XSS:           true,
				PathTraversal: true,
				CmdInjection:  true,
			},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if h.mw == nil {
		t.Error("Provision did not set mw field")
	}
}

// ---------------------------------------------------------------------------
// ServeHTTP — block and detect modes
// ---------------------------------------------------------------------------

func TestWAFEngineHandler_BlocksAttackInBlockMode(t *testing.T) {
	h := &WAFEngineHandler{
		Config: WAFEngineHandlerConfig{
			Mode: "block",
			Rules: WAFEngineHandlerRulesConfig{
				SQLInjection:  true,
				XSS:           true,
				PathTraversal: true,
				CmdInjection:  true,
			},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	_ = h.ServeHTTP(rr, req, next)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestWAFEngineHandler_PassesThroughInDetectMode(t *testing.T) {
	h := &WAFEngineHandler{
		Config: WAFEngineHandlerConfig{
			Mode: "detect",
			Rules: WAFEngineHandlerRulesConfig{
				SQLInjection:  true,
				XSS:           true,
				PathTraversal: true,
				CmdInjection:  true,
			},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	_ = h.ServeHTTP(rr, req, next)

	if rr.Code != http.StatusOK {
		t.Errorf("detect mode: status = %d, want 200", rr.Code)
	}
}

func TestWAFEngineHandler_CleanRequest_Passes(t *testing.T) {
	h := &WAFEngineHandler{
		Config: WAFEngineHandlerConfig{
			Mode: "block",
			Rules: WAFEngineHandlerRulesConfig{
				SQLInjection:  true,
				XSS:           true,
				PathTraversal: true,
				CmdInjection:  true,
			},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/products?q=laptop", nil)
	rr := httptest.NewRecorder()

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	_ = h.ServeHTTP(rr, req, next)

	if rr.Code != http.StatusOK {
		t.Errorf("clean request: status = %d, want 200", rr.Code)
	}
}

func TestWAFEngineHandler_EmptyMode_DefaultsToDetect(t *testing.T) {
	h := &WAFEngineHandler{
		Config: WAFEngineHandlerConfig{
			Mode: "", // empty — should default to detect (log-only, pass through)
			Rules: WAFEngineHandlerRulesConfig{
				SQLInjection: true,
			},
		},
	}
	if err := h.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/search?q=1+UNION+SELECT+1,2,3", nil)
	rr := httptest.NewRecorder()

	next := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusOK)
		return nil
	})
	_ = h.ServeHTTP(rr, req, next)

	// In detect mode attacks are logged but not blocked — request passes through.
	if rr.Code != http.StatusOK {
		t.Errorf("empty mode defaults to detect (pass-through): status = %d, want 200", rr.Code)
	}
}
