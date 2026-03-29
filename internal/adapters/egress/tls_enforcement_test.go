package egress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

// newTLSTestProxy creates a Proxy with the given AllowInsecure flag and routes,
// wired to the given HTTP client. Starts listening on an OS-assigned port.
func newTLSTestProxy(t *testing.T, allowInsecure bool, routes []domainegress.Route, client *http.Client) *egressadapter.Proxy {
	t.Helper()
	resolver := egressadapter.NewRouteResolver(routes)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyAllow,
		DefaultTimeout: 5 * time.Second,
		Routes:         routes,
		AllowInsecure:  allowInsecure,
	}
	return egressadapter.NewProxy(cfg, resolver, client, nil)
}

// TestHandleRequest_TLSEnforcement verifies that plain HTTP egress targets are
// rejected by default and allowed only when the appropriate flag is set.
func TestHandleRequest_TLSEnforcement(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	// upstream.URL is http:// because httptest.NewServer is plain HTTP.

	tests := []struct {
		name         string
		globalAllow  bool
		routeAllow   bool
		targetIsHTTP bool
		wantErr      error
		wantNoErr    bool
	}{
		{
			name:         "HTTPS target always allowed (global deny insecure)",
			globalAllow:  false,
			routeAllow:   false,
			targetIsHTTP: false,
			wantNoErr:    true,
		},
		{
			name:         "HTTP target blocked by default",
			globalAllow:  false,
			routeAllow:   false,
			targetIsHTTP: true,
			wantErr:      egressadapter.ErrInsecureURL,
		},
		{
			name:         "HTTP target allowed when global allow_insecure is true",
			globalAllow:  true,
			routeAllow:   false,
			targetIsHTTP: true,
			wantNoErr:    true,
		},
		{
			name:         "HTTP target allowed when route allow_insecure is true",
			globalAllow:  false,
			routeAllow:   true,
			targetIsHTTP: true,
			wantNoErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				targetURL string
				routes    []domainegress.Route
				client    *http.Client
			)

			if tt.targetIsHTTP {
				// Point to the plain-HTTP test server.
				targetURL = upstream.URL + "/test"
				routeOpts := []domainegress.RouteOption{}
				if tt.routeAllow {
					routeOpts = append(routeOpts, domainegress.WithAllowInsecure(true))
				}
				route, err := domainegress.NewRoute("test", upstream.URL+"/test", routeOpts...)
				if err != nil {
					t.Fatalf("NewRoute: %v", err)
				}
				routes = []domainegress.Route{route}
				client = upstream.Client()
			} else {
				// Use an HTTPS test server.
				tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}))
				defer tlsServer.Close()
				targetURL = tlsServer.URL + "/test"
				route, err := domainegress.NewRoute("test", tlsServer.URL+"/test")
				if err != nil {
					t.Fatalf("NewRoute: %v", err)
				}
				routes = []domainegress.Route{route}
				client = tlsServer.Client()
			}

			proxy := newTLSTestProxy(t, tt.globalAllow, routes, client)

			req, err := domainegress.NewEgressRequest("GET", targetURL, nil, nil)
			if err != nil {
				t.Fatalf("NewEgressRequest: %v", err)
			}

			_, handleErr := proxy.HandleRequest(context.Background(), req)

			if tt.wantNoErr {
				if handleErr != nil {
					t.Errorf("HandleRequest() unexpected error: %v", handleErr)
				}
				return
			}
			if handleErr == nil {
				t.Fatal("HandleRequest() expected an error, got nil")
			}
			if tt.wantErr != nil && handleErr != tt.wantErr {
				t.Errorf("HandleRequest() error = %v, want %v", handleErr, tt.wantErr)
			}
		})
	}
}

// TestHandleRequest_TLSEnforcement_UnmatchedRoute verifies that unmatched HTTP
// requests (policy=allow) are still blocked by the TLS enforcement check.
func TestHandleRequest_TLSEnforcement_UnmatchedRoute(t *testing.T) {
	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyAllow,
		DefaultTimeout: 5 * time.Second,
		AllowInsecure:  false,
	}
	proxy := egressadapter.NewProxy(cfg, resolver, nil, nil)

	req, err := domainegress.NewEgressRequest("GET", "http://some-external-api.example.com/data", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest: %v", err)
	}

	_, handleErr := proxy.HandleRequest(context.Background(), req)
	if handleErr == nil {
		t.Fatal("HandleRequest() expected ErrInsecureURL, got nil")
	}
	if handleErr != egressadapter.ErrInsecureURL {
		t.Errorf("HandleRequest() error = %v, want ErrInsecureURL", handleErr)
	}
}

// TestHTTPHandler_TLSEnforcement_Returns400 verifies that the HTTP handler
// returns 400 Bad Request when the egress target is plain HTTP and insecure
// requests are not allowed.
func TestHTTPHandler_TLSEnforcement_Returns400(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for TLS-blocked requests")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	resolver := egressadapter.NewRouteResolver(nil)
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyAllow,
		DefaultTimeout: 5 * time.Second,
		AllowInsecure:  false,
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
	// Target a plain HTTP URL — the TLS enforcement must block it.
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/api/data")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d (plain HTTP must be rejected)", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestHTTPHandler_TLSEnforcement_AllowInsecure_Passes verifies that the HTTP
// handler forwards plain HTTP requests when allow_insecure is true.
func TestHTTPHandler_TLSEnforcement_AllowInsecure_Passes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("insecure-ok"))
	}))
	defer upstream.Close()

	route := newTestRoute(t, "test", upstream.URL+"/api/*")
	resolver := egressadapter.NewRouteResolver([]domainegress.Route{route})
	cfg := egressadapter.ProxyConfig{
		Listen:         "127.0.0.1:0",
		DefaultPolicy:  domainegress.PolicyDeny,
		DefaultTimeout: 5 * time.Second,
		Routes:         []domainegress.Route{route},
		AllowInsecure:  true,
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
	httpReq.Header.Set("X-Egress-URL", upstream.URL+"/api/resource")

	resp, err := proxyClient.Do(httpReq)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
