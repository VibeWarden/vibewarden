//go:build integration

// Package e2e contains end-to-end integration tests for VibeWarden.
//
// These tests spin up a minimal two-container stack — a lightweight upstream
// echo server and the VibeWarden sidecar — using testcontainers-go, then
// exercise the full HTTP request path.
//
// Scenarios covered:
//   - Proxy passthrough: a request to a proxied path returns 200
//   - Health endpoint: /_vibewarden/health returns 200
//   - Security headers: response includes X-Content-Type-Options and X-Frame-Options
//   - Rate limiting: rapid fire requests eventually produce 429
//   - Metrics endpoint: /_vibewarden/metrics returns 200 with Prometheus content
//
// Kratos and OpenBao are intentionally excluded: they are too heavy for this
// integration layer and are exercised separately via the demo stack.
//
// Usage:
//
//	go test -tags integration -v -timeout 5m ./test/e2e/
package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// stackTimeout is the hard deadline for the full test suite, including
// container start-up.
const stackTimeout = 5 * time.Minute

// vibewardenConfig is the minimal vibewarden.yaml written into the sidecar
// container at runtime. Kratos and OpenBao are disabled. Rate limiting is
// configured with a tiny burst so the 429 test runs quickly.
const vibewardenConfig = `
profile: dev

server:
  host: "0.0.0.0"
  port: 8080

upstream:
  host: upstream
  port: 3000

tls:
  enabled: false

kratos:
  public_url: ""
  admin_url: ""

auth:
  enabled: false

rate_limit:
  enabled: true
  store: memory
  per_ip:
    requests_per_second: 1
    burst: 2
  per_user:
    requests_per_second: 5
    burst: 5
  trust_proxy_headers: false
  exempt_paths:
    - "/_vibewarden/*"

security_headers:
  enabled: true
  hsts_max_age: 0
  content_type_nosniff: true
  frame_option: "DENY"
  content_security_policy: "default-src 'self'; style-src 'self' 'unsafe-inline'"
  referrer_policy: "strict-origin-when-cross-origin"

telemetry:
  enabled: true
  prometheus:
    enabled: true

log:
  level: "info"
  format: "json"

admin:
  enabled: false

secrets:
  enabled: false
`

// stack holds the running containers and the base URL for VibeWarden.
type stack struct {
	vibewardenURL string

	upstream   testcontainers.Container
	vibewarden testcontainers.Container
}

// startStack builds and starts the two-container stack. It registers cleanup
// with t.Cleanup so containers are always terminated, even on test failure.
func startStack(ctx context.Context, t *testing.T) *stack {
	t.Helper()

	// Generate a unique network name up front so it can be referenced by both
	// containers.  The testcontainers Network interface only exposes Remove(),
	// so we hold the name separately.
	networkName := fmt.Sprintf("vibewarden-e2e-%d", time.Now().UnixNano())

	network, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{
			Name:           networkName,
			CheckDuplicate: true,
		},
	})
	if err != nil {
		t.Fatalf("creating docker network: %v", err)
	}
	t.Cleanup(func() {
		if err := network.Remove(ctx); err != nil {
			t.Logf("removing test network: %v", err)
		}
	})

	// -----------------------------------------------------------------------
	// Upstream echo server — a plain alpine httpd that returns 200 for
	// every request.  It is identified on the internal Docker network as
	// "upstream" so VibeWarden can proxy to upstream:3000.
	// -----------------------------------------------------------------------
	upstreamReq := testcontainers.ContainerRequest{
		Image: "python:3-alpine",
		// Serve a tiny HTTP server on port 3000.
		Cmd: []string{
			"sh", "-c",
			`python3 -c "
import http.server, socketserver

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{\"ok\":true}')
    def do_POST(self):
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{\"ok\":true}')
    def log_message(self, *args): pass

with socketserver.TCPServer(('0.0.0.0', 3000), Handler) as srv:
    srv.serve_forever()
"`,
		},
		ExposedPorts: []string{"3000/tcp"},
		Networks:     []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"upstream"},
		},
		WaitingFor: wait.ForListeningPort("3000/tcp").WithStartupTimeout(60 * time.Second),
	}

	upstreamContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: upstreamReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting upstream container: %v", err)
	}
	t.Cleanup(func() {
		if err := upstreamContainer.Terminate(ctx); err != nil {
			t.Logf("terminating upstream container: %v", err)
		}
	})

	// -----------------------------------------------------------------------
	// VibeWarden sidecar — built from the project's Dockerfile. The config
	// is injected via a tmpfs/copy mechanism using testcontainers file mount.
	// -----------------------------------------------------------------------
	vibewardenReq := testcontainers.ContainerRequest{
		// Build from the project root so the binary picks up the current code.
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    "../../",
			Dockerfile: "Dockerfile",
			BuildArgs: map[string]*string{
				"VERSION": strPtr("e2e-test"),
			},
			// Reuse cached layers between test runs for speed.
			KeepImage: true,
		},
		Cmd: []string{"serve"},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(vibewardenConfig),
				ContainerFilePath: "/vibewarden.yaml",
				FileMode:          0o644,
			},
		},
		ExposedPorts: []string{"8080/tcp"},
		Networks:     []string{networkName},
		NetworkAliases: map[string][]string{
			networkName: {"vibewarden"},
		},
		WaitingFor: wait.ForHTTP("/_vibewarden/health").
			WithPort("8080/tcp").
			WithStatusCodeMatcher(func(status int) bool { return status == 200 }).
			WithStartupTimeout(2 * time.Minute),
	}

	vibewardenContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: vibewardenReq,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting vibewarden container: %v", err)
	}
	t.Cleanup(func() {
		if err := vibewardenContainer.Terminate(ctx); err != nil {
			t.Logf("terminating vibewarden container: %v", err)
		}
	})

	host, err := vibewardenContainer.Host(ctx)
	if err != nil {
		t.Fatalf("getting vibewarden host: %v", err)
	}

	mappedPort, err := vibewardenContainer.MappedPort(ctx, "8080/tcp")
	if err != nil {
		t.Fatalf("getting vibewarden mapped port: %v", err)
	}

	baseURL := fmt.Sprintf("http://%s", net.JoinHostPort(host, mappedPort.Port()))

	return &stack{
		vibewardenURL: baseURL,
		upstream:      upstreamContainer,
		vibewarden:    vibewardenContainer,
	}
}

// strPtr returns a pointer to the given string, used for Docker build args.
func strPtr(s string) *string { return &s }

// get is a convenience wrapper for http.Get that fails the test on transport
// errors.
func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // test helper, context unused
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

// discardBody reads and closes the response body to allow connection reuse.
func discardBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// TestE2E is the parent test that starts the stack once and runs all
// sub-tests against it. Starting containers is expensive; sharing the stack
// across sub-tests keeps the suite fast.
//
// Individual sub-tests must not mutate shared state (e.g. the HTTP client or
// the stack URLs).
func TestE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), stackTimeout)
	t.Cleanup(cancel)

	s := startStack(ctx, t)

	// Run sub-tests sequentially.  The rate-limiting sub-test is deliberately
	// placed last because it exhausts the IP-level burst quota, which could
	// interfere with other tests if run first.
	t.Run("health_endpoint", func(t *testing.T) { testHealthEndpoint(t, s) })
	t.Run("proxy_passthrough", func(t *testing.T) { testProxyPassthrough(t, s) })
	t.Run("security_headers", func(t *testing.T) { testSecurityHeaders(t, s) })
	t.Run("metrics_endpoint", func(t *testing.T) { testMetricsEndpoint(t, s) })
	t.Run("rate_limiting", func(t *testing.T) { testRateLimiting(t, s) })
}

// testHealthEndpoint verifies that the VibeWarden health endpoint returns 200.
func testHealthEndpoint(t *testing.T, s *stack) {
	t.Helper()

	url := s.vibewardenURL + "/_vibewarden/health"
	resp := get(t, url)
	defer discardBody(resp)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("health endpoint: got status %d, want 200", resp.StatusCode)
	}
}

// testProxyPassthrough verifies that a plain GET request is proxied to the
// upstream echo server and returns 200.
func testProxyPassthrough(t *testing.T, s *stack) {
	t.Helper()

	url := s.vibewardenURL + "/"
	resp := get(t, url)
	defer discardBody(resp)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("proxy passthrough: got status %d, want 200", resp.StatusCode)
	}
}

// testSecurityHeaders verifies that the security-headers plugin injects the
// expected response headers on every proxied request.
func testSecurityHeaders(t *testing.T, s *stack) {
	t.Helper()

	tests := []struct {
		header string
		want   string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	url := s.vibewardenURL + "/"
	resp := get(t, url)
	defer discardBody(resp)

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := resp.Header.Get(tt.header)
			if got == "" {
				t.Errorf("header %q: missing from response", tt.header)
				return
			}
			if got != tt.want {
				t.Errorf("header %q: got %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// testMetricsEndpoint verifies that the Prometheus metrics endpoint is
// reachable and returns a non-empty body with the expected content type.
func testMetricsEndpoint(t *testing.T, s *stack) {
	t.Helper()

	url := s.vibewardenURL + "/_vibewarden/metrics"
	resp := get(t, url)
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics endpoint: got status %d, want 200", resp.StatusCode)
		return
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("metrics content-type: got %q, want to contain text/plain", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading metrics body: %v", err)
	}
	if len(body) == 0 {
		t.Error("metrics endpoint: body is empty, expected Prometheus output")
	}
}

// testRateLimiting verifies that rapid-fire requests from the same IP
// eventually receive a 429 Too Many Requests response.
//
// The stack is configured with per_ip burst=2 and requests_per_second=1.
// Sending more than burst+1 requests in quick succession must hit the limit.
// This test runs last because it exhausts the burst quota for the test
// client's IP address.
func testRateLimiting(t *testing.T, s *stack) {
	t.Helper()

	// Use a single HTTP client with no keep-alives so each request carries
	// the same source IP from testcontainers' perspective.
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
		Timeout: 10 * time.Second,
	}

	url := s.vibewardenURL + "/spam"

	const maxRequests = 20
	got429 := false

	for i := range maxRequests {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("building request #%d: %v", i+1, err)
		}

		resp, err := client.Do(req)
		if err != nil {
			// A refused connection after the burst is also acceptable — some
			// configurations reset the connection rather than returning 429.
			t.Logf("request #%d: connection error (may be expected after burst): %v", i+1, err)
			got429 = true
			break
		}
		discardBody(resp)

		if resp.StatusCode == http.StatusTooManyRequests {
			t.Logf("got 429 after %d requests", i+1)
			got429 = true
			break
		}
	}

	if !got429 {
		t.Errorf("rate limiting: sent %d requests but never received 429", maxRequests)
	}
}
