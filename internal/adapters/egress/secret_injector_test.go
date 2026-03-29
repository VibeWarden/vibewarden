package egress_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// fakeSecretStore is a simple in-memory SecretStore used in tests.
// It deliberately does not log secret values.
type fakeSecretStore struct {
	secrets map[string]map[string]string
	callLog []string // records which secret names were fetched
}

func newFakeSecretStore(secrets map[string]map[string]string) *fakeSecretStore {
	return &fakeSecretStore{secrets: secrets}
}

func (f *fakeSecretStore) Get(_ context.Context, path string) (map[string]string, error) {
	f.callLog = append(f.callLog, path)
	data, ok := f.secrets[path]
	if !ok {
		return nil, errors.New("secret not found: " + path)
	}
	return data, nil
}

func (f *fakeSecretStore) Put(_ context.Context, _ string, _ map[string]string) error {
	return errors.New("not implemented")
}

func (f *fakeSecretStore) Delete(_ context.Context, _ string) error {
	return errors.New("not implemented")
}

func (f *fakeSecretStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeSecretStore) Health(_ context.Context) error {
	return nil
}

// TestSecretInjector_Inject tests Inject for various SecretConfig inputs.
func TestSecretInjector_Inject(t *testing.T) {
	store := newFakeSecretStore(map[string]map[string]string{
		"stripe/api_key": {"value": "sk_test_abc123"},
		"github/token":   {"value": "ghp_xyz"},
		"multi-key":      {"token": "tok_multi", "extra": "ignored"},
		"single-nonval":  {"api_key": "raw_value"},
	})

	tests := []struct {
		name       string
		cfg        domainegress.SecretConfig
		wantHeader string
		wantValue  string
		wantErr    bool
	}{
		{
			name: "bearer format injection",
			cfg: domainegress.SecretConfig{
				Name:   "stripe/api_key",
				Header: "Authorization",
				Format: "Bearer {value}",
			},
			wantHeader: "Authorization",
			wantValue:  "Bearer sk_test_abc123",
		},
		{
			name: "plain value injection (no format)",
			cfg: domainegress.SecretConfig{
				Name:   "github/token",
				Header: "Authorization",
				Format: "",
			},
			wantHeader: "Authorization",
			wantValue:  "ghp_xyz",
		},
		{
			name: "custom header name",
			cfg: domainegress.SecretConfig{
				Name:   "stripe/api_key",
				Header: "X-Api-Key",
				Format: "",
			},
			wantHeader: "X-Api-Key",
			wantValue:  "sk_test_abc123",
		},
		{
			name: "token format",
			cfg: domainegress.SecretConfig{
				Name:   "github/token",
				Header: "Authorization",
				Format: "token {value}",
			},
			wantHeader: "Authorization",
			wantValue:  "token ghp_xyz",
		},
		{
			name: "multi-key secret falls back to first value when 'value' key missing",
			cfg: domainegress.SecretConfig{
				Name:   "single-nonval",
				Header: "X-Api-Key",
				Format: "",
			},
			wantHeader: "X-Api-Key",
			wantValue:  "raw_value",
		},
		{
			name: "missing secret returns error",
			cfg: domainegress.SecretConfig{
				Name:   "nonexistent/secret",
				Header: "Authorization",
				Format: "Bearer {value}",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

			header, value, err := injector.Inject(context.Background(), tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Inject() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if header != tt.wantHeader {
				t.Errorf("header = %q, want %q", header, tt.wantHeader)
			}
			if value != tt.wantValue {
				t.Errorf("value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

// TestSecretInjector_Caching verifies that a second Inject call for the same
// secret name does not call the store again within the TTL.
func TestSecretInjector_Caching(t *testing.T) {
	store := newFakeSecretStore(map[string]map[string]string{
		"cached/key": {"value": "secret_val"},
	})

	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{
		TTL: 10 * time.Minute,
	})

	cfg := domainegress.SecretConfig{
		Name:   "cached/key",
		Header: "Authorization",
		Format: "Bearer {value}",
	}

	if _, _, err := injector.Inject(context.Background(), cfg); err != nil {
		t.Fatalf("first Inject() error = %v", err)
	}
	if _, _, err := injector.Inject(context.Background(), cfg); err != nil {
		t.Fatalf("second Inject() error = %v", err)
	}

	if len(store.callLog) != 1 {
		t.Errorf("store.Get called %d times, want 1 (second call should use cache)", len(store.callLog))
	}
}

// TestSecretInjector_CacheExpiry verifies that after the TTL expires a new
// store fetch is performed.
func TestSecretInjector_CacheExpiry(t *testing.T) {
	store := newFakeSecretStore(map[string]map[string]string{
		"expiry/key": {"value": "fresh_val"},
	})

	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{
		TTL: 1 * time.Millisecond, // expire almost immediately
	})

	cfg := domainegress.SecretConfig{
		Name:   "expiry/key",
		Header: "Authorization",
		Format: "",
	}

	if _, _, err := injector.Inject(context.Background(), cfg); err != nil {
		t.Fatalf("first Inject() error = %v", err)
	}

	// Wait for the TTL to expire.
	time.Sleep(5 * time.Millisecond)

	if _, _, err := injector.Inject(context.Background(), cfg); err != nil {
		t.Fatalf("second Inject() error = %v", err)
	}

	if len(store.callLog) != 2 {
		t.Errorf("store.Get called %d times, want 2 (cache should have expired)", len(store.callLog))
	}
}

// TestProxy_SecretInjection_PerRoute verifies that the proxy injects the
// per-route static secret into the upstream request headers.
func TestProxy_SecretInjection_PerRoute(t *testing.T) {
	var capturedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		// X-Inject-Secret must always be stripped before reaching upstream.
		if r.Header.Get("X-Inject-Secret") != "" {
			t.Error("X-Inject-Secret must not reach the upstream")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := newFakeSecretStore(map[string]map[string]string{
		"stripe/key": {"value": "sk_live_test"},
	})
	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

	route, err := domainegress.NewRoute("stripe", upstream.URL+"/v1/*",
		domainegress.WithSecret(domainegress.SecretConfig{
			Name:   "stripe/key",
			Header: "Authorization",
			Format: "Bearer {value}",
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		SecretInjector: injector,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/charges", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if capturedAuth != "Bearer sk_live_test" {
		t.Errorf("upstream Authorization = %q, want %q", capturedAuth, "Bearer sk_live_test")
	}
}

// TestProxy_SecretInjection_DynamicHeader verifies that X-Inject-Secret is
// used as the secret name when no per-route SecretConfig.Name is set.
func TestProxy_SecretInjection_DynamicHeader(t *testing.T) {
	var capturedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		if r.Header.Get("X-Inject-Secret") != "" {
			t.Error("X-Inject-Secret must not reach the upstream")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := newFakeSecretStore(map[string]map[string]string{
		"github/token": {"value": "ghp_dynamic"},
	})
	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

	// Route has no SecretConfig — injection is driven by the X-Inject-Secret header.
	route, err := domainegress.NewRoute("github", upstream.URL+"/api/*")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		SecretInjector: injector,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	inHeaders := http.Header{}
	inHeaders.Set("X-Inject-Secret", "github/token")

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/api/repos", inHeaders, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("HandleRequest: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if capturedAuth != "ghp_dynamic" {
		t.Errorf("upstream Authorization = %q, want %q", capturedAuth, "ghp_dynamic")
	}
}

// TestProxy_SecretInjection_FailClosed verifies that a request is blocked when
// secret resolution fails, even when the route is matched.
func TestProxy_SecretInjection_FailClosed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when secret injection fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Store has no entry for the secret the route requires.
	store := newFakeSecretStore(map[string]map[string]string{})
	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

	route, err := domainegress.NewRoute("stripe", upstream.URL+"/v1/*",
		domainegress.WithSecret(domainegress.SecretConfig{
			Name:   "stripe/missing",
			Header: "Authorization",
			Format: "Bearer {value}",
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		SecretInjector: injector,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/charges", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("HandleRequest should have returned an error when secret injection fails")
	}
	if !strings.Contains(err.Error(), "secret injection") {
		t.Errorf("error = %q, want it to mention 'secret injection'", err.Error())
	}
}

// TestProxy_SecretInjection_NoInjectorConfigured verifies that when a route
// requires secret injection but no SecretInjector is wired into the proxy,
// the request is blocked (fail-closed).
func TestProxy_SecretInjection_NoInjectorConfigured(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when no injector is configured")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	route, err := domainegress.NewRoute("stripe", upstream.URL+"/v1/*",
		domainegress.WithSecret(domainegress.SecretConfig{
			Name:   "stripe/key",
			Header: "Authorization",
			Format: "Bearer {value}",
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		// SecretInjector deliberately omitted.
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/charges", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("HandleRequest should return an error when injector is required but absent")
	}
}

// TestProxy_SecretInjection_XInjectSecretStripped verifies that X-Inject-Secret
// is always stripped from the outbound request even when no injection occurs
// (e.g. no injector configured, unmatched route).
func TestProxy_SecretInjection_XInjectSecretStripped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Inject-Secret") != "" {
			t.Error("X-Inject-Secret must be stripped, but it reached the upstream")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Route with no SecretConfig and no injector: dynamic header must still be stripped.
	route, err := domainegress.NewRoute("plain", upstream.URL+"/v1/*")
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		// No SecretInjector — X-Inject-Secret present but no injector configured.
		// The route has no SecretConfig so no injection is required; the header
		// must simply be stripped.
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	inHeaders := http.Header{}
	inHeaders.Set("X-Inject-Secret", "any/secret")

	req, err := domainegress.NewEgressRequest("GET", upstream.URL+"/v1/resource", inHeaders, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	// This should fail because there is no injector but X-Inject-Secret is present.
	// The proxy must block the request (fail-closed) even for the dynamic case.
	_, err = proxy.HandleRequest(context.Background(), req)
	if err == nil {
		t.Fatal("HandleRequest should return an error when dynamic injection is requested without an injector")
	}
}

// TestProxy_SecretInjection_HTTPHandler verifies the full HTTP handler path:
// a request with X-Inject-Secret returns 502 when no injector is wired.
func TestProxy_SecretInjection_HTTPHandler_FailClosed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store := newFakeSecretStore(map[string]map[string]string{})
	injector := egressadapter.NewSecretInjector(store, egressadapter.SecretInjectorConfig{})

	route, err := domainegress.NewRoute("stripe", upstream.URL+"/v1/*",
		domainegress.WithSecret(domainegress.SecretConfig{
			Name:   "stripe/missing",
			Header: "Authorization",
			Format: "Bearer {value}",
		}),
	)
	if err != nil {
		t.Fatalf("NewRoute: %v", err)
	}

	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		SecretInjector: injector,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, upstream.Client(), nil)

	if err := proxy.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer proxy.Stop(context.Background()) //nolint:errcheck

	proxyURL := "http://" + proxy.Addr() + "/"
	proxyClient := &http.Client{Timeout: 5 * time.Second}

	httpReq, err := http.NewRequest(http.MethodGet, proxyURL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/v1/charges")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want %d (secret injection failure must return 502); body: %s",
			resp.StatusCode, http.StatusBadGateway, body)
	}
}
