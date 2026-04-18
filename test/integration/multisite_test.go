//go:build integration

// Package integration contains integration tests for VibeWarden.
//
// TestMultiSite validates end-to-end multi-app subdomain routing by starting
// two upstream HTTP containers and a VibeWarden sidecar container, then
// verifying that Host-header-based routing, TLS, security headers, and error
// isolation all work correctly.
//
// Prerequisites:
//   - Docker daemon running
//   - ghcr.io/vibewarden/vibewarden:latest built locally (make demo-build)
//
// Run:
//
//	go test -race -tags integration ./test/integration/ -run TestMultiSite -v
package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// sidecarImage is the Docker image used for the VibeWarden sidecar.
	// It must be built locally before running integration tests (make demo-build).
	sidecarImage = "ghcr.io/vibewarden/vibewarden:latest"

	// echoImage is the Docker image used for upstream app containers.
	// hashicorp/http-echo is MIT-licensed and returns a fixed text response.
	echoImage = "hashicorp/http-echo:latest"

	// sidecarPort is the HTTPS listen port inside the sidecar container.
	sidecarPort = "8443"

	// echoPort is the listen port inside each echo container.
	echoPort = "5678"
)

// TestMultiSite validates end-to-end multi-app subdomain routing through the
// VibeWarden sidecar. It creates two upstream app containers, configures a
// multi-site sidecar, and verifies:
//
//  1. Host-header routing: requests with Host: app1.local reach app1,
//     requests with Host: app2.local reach app2.
//  2. Unknown host rejection: requests with an unknown Host header are rejected.
//  3. TLS: self-signed certificates are served per domain.
//  4. Security headers: standard security headers are present on responses.
//  5. Error isolation: a broken app3 config does not affect app1 and app2.
func TestMultiSite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// -----------------------------------------------------------------------
	// Step 1: Create a shared Docker network.
	// -----------------------------------------------------------------------
	testNet, err := network.New(ctx)
	if err != nil {
		t.Fatalf("creating Docker network: %v", err)
	}
	t.Cleanup(func() {
		if cleanupErr := testNet.Remove(ctx); cleanupErr != nil {
			t.Logf("warning: removing Docker network: %v", cleanupErr)
		}
	})

	netName := testNet.Name

	// -----------------------------------------------------------------------
	// Step 2: Start upstream app containers.
	// -----------------------------------------------------------------------
	app1, err := startEchoContainer(ctx, t, netName, "app1-echo", `{"app":"app1"}`)
	if err != nil {
		t.Fatalf("starting app1 container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := app1.Terminate(ctx); termErr != nil {
			t.Logf("warning: terminating app1: %v", termErr)
		}
	})

	app2, err := startEchoContainer(ctx, t, netName, "app2-echo", `{"app":"app2"}`)
	if err != nil {
		t.Fatalf("starting app2 container: %v", err)
	}
	t.Cleanup(func() {
		if termErr := app2.Terminate(ctx); termErr != nil {
			t.Logf("warning: terminating app2: %v", termErr)
		}
	})

	// -----------------------------------------------------------------------
	// Step 3: Write multi-site configuration to a temp directory.
	// -----------------------------------------------------------------------
	configDir := t.TempDir()
	writeSidecarConfig(t, configDir)

	// -----------------------------------------------------------------------
	// Step 4: Start the VibeWarden sidecar container.
	// -----------------------------------------------------------------------
	sidecar, err := startSidecarContainer(ctx, t, netName, configDir)
	if err != nil {
		t.Fatalf("starting sidecar container: %v", err)
	}
	t.Cleanup(func() {
		// Dump sidecar logs on failure for debugging.
		if t.Failed() {
			logs, logErr := sidecar.Logs(ctx)
			if logErr == nil {
				logBytes, _ := io.ReadAll(logs)
				t.Logf("sidecar logs:\n%s", string(logBytes))
			}
		}
		if termErr := sidecar.Terminate(ctx); termErr != nil {
			t.Logf("warning: terminating sidecar: %v", termErr)
		}
	})

	// Resolve the sidecar's mapped HTTPS port on the host.
	sidecarHost, err := sidecar.Host(ctx)
	if err != nil {
		t.Fatalf("getting sidecar host: %v", err)
	}
	sidecarMappedPort, err := sidecar.MappedPort(ctx, sidecarPort+"/tcp")
	if err != nil {
		t.Fatalf("getting sidecar mapped port: %v", err)
	}
	baseURL := fmt.Sprintf("https://%s:%s", sidecarHost, sidecarMappedPort.Port())

	t.Logf("sidecar reachable at %s", baseURL)

	// newTLSClient builds an HTTP client whose TLS handshake uses the
	// given serverName for SNI. This is necessary because the test connects
	// to localhost:<mapped-port> while the server only holds certificates
	// for app1.local / app2.local. Without the correct SNI, Caddy cannot
	// select the right certificate and the handshake fails.
	newTLSClient := func(serverName string) *http.Client {
		return &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					ServerName:         serverName,
					InsecureSkipVerify: true, //nolint:gosec // self-signed certs in test
				},
			},
		}
	}

	clientApp1 := newTLSClient("app1.local")
	clientApp2 := newTLSClient("app2.local")

	// -----------------------------------------------------------------------
	// Step 5: Test Host-header routing.
	// -----------------------------------------------------------------------
	t.Run("app1_routing", func(t *testing.T) {
		body := doRequest(t, clientApp1, baseURL+"/", "app1.local")
		if !strings.Contains(body, `{"app":"app1"}`) {
			t.Errorf("app1 routing: got body %q, want to contain %q", body, `{"app":"app1"}`)
		}
	})

	t.Run("app2_routing", func(t *testing.T) {
		body := doRequest(t, clientApp2, baseURL+"/", "app2.local")
		if !strings.Contains(body, `{"app":"app2"}`) {
			t.Errorf("app2 routing: got body %q, want to contain %q", body, `{"app":"app2"}`)
		}
	})

	// -----------------------------------------------------------------------
	// Step 6: Test unknown host rejection.
	// -----------------------------------------------------------------------
	t.Run("unknown_host_rejected", func(t *testing.T) {
		unknownClient := newTLSClient("unknown.local")

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
		if err != nil {
			t.Fatalf("creating request: %v", err)
		}
		req.Host = "unknown.local"

		resp, err := unknownClient.Do(req)
		if err != nil {
			// A TLS error or connection reset is acceptable for an unknown
			// host — Caddy has no certificate for "unknown.local" so the
			// handshake may fail entirely.
			t.Logf("unknown host request returned error (acceptable): %v", err)
			return
		}
		defer resp.Body.Close()

		// Caddy returns 404 or an empty response for unmatched hosts.
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusOK {
			t.Logf("unknown host: got status %d", resp.StatusCode)
		}

		// The key assertion: the response must NOT contain app1 or app2 content.
		bodyBytes, _ := io.ReadAll(resp.Body)
		body := string(bodyBytes)
		if strings.Contains(body, `"app1"`) || strings.Contains(body, `"app2"`) {
			t.Errorf("unknown host leaked app content: %s", body)
		}
	})

	// -----------------------------------------------------------------------
	// Step 7: Test TLS (self-signed certs per domain).
	// -----------------------------------------------------------------------
	t.Run("tls_app1", func(t *testing.T) {
		verifyTLS(t, sidecarHost, sidecarMappedPort.Port(), "app1.local")
	})

	t.Run("tls_app2", func(t *testing.T) {
		verifyTLS(t, sidecarHost, sidecarMappedPort.Port(), "app2.local")
	})

	// -----------------------------------------------------------------------
	// Step 8: Test security headers.
	// -----------------------------------------------------------------------
	t.Run("security_headers_app1", func(t *testing.T) {
		verifySecurityHeaders(t, clientApp1, baseURL, "app1.local")
	})

	t.Run("security_headers_app2", func(t *testing.T) {
		verifySecurityHeaders(t, clientApp2, baseURL, "app2.local")
	})

	// -----------------------------------------------------------------------
	// Step 9: Test error isolation — a broken app3 config should not prevent
	// app1 and app2 from serving.
	// -----------------------------------------------------------------------
	t.Run("error_isolation", func(t *testing.T) {
		testErrorIsolation(t, ctx, clientApp1, clientApp2, baseURL, configDir)
	})
}

// startEchoContainer creates and starts a hashicorp/http-echo container that
// returns the given text on every HTTP request.
func startEchoContainer(ctx context.Context, t *testing.T, netName, alias, text string) (testcontainers.Container, error) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:    echoImage,
		Cmd:      []string{"-text=" + text, "-listen=:" + echoPort},
		Networks: []string{netName},
		NetworkAliases: map[string][]string{
			netName: {alias},
		},
		WaitingFor: wait.ForHTTP("/").WithPort(echoPort + "/tcp").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating echo container %q: %w", alias, err)
	}

	t.Logf("echo container %q started", alias)
	return container, nil
}

// writeSidecarConfig writes the multi-site configuration files to configDir.
// The layout matches what isMultiSiteDir expects:
//
//	configDir/
//	  global.yaml
//	  sites/
//	    app1/vibewarden.yaml
//	    app2/vibewarden.yaml
func writeSidecarConfig(t *testing.T, configDir string) {
	t.Helper()

	// global.yaml — listen on 0.0.0.0:8443, admin token for test.
	globalYAML := fmt.Sprintf(`admin_token: "test-admin-token"
listen_host: "0.0.0.0"
listen_port: %s
log_level: "debug"
`, sidecarPort)

	if err := os.WriteFile(filepath.Join(configDir, "global.yaml"), []byte(globalYAML), 0o644); err != nil {
		t.Fatalf("writing global.yaml: %v", err)
	}

	sitesDir := filepath.Join(configDir, "sites")
	if err := os.MkdirAll(sitesDir, 0o755); err != nil {
		t.Fatalf("creating sites dir: %v", err)
	}

	// app1 site config — proxies to the app1-echo container.
	writeAppConfig(t, sitesDir, "app1", "app1.local", "app1-echo", echoPort)

	// app2 site config — proxies to the app2-echo container.
	writeAppConfig(t, sitesDir, "app2", "app2.local", "app2-echo", echoPort)
}

// writeAppConfig writes a per-site vibewarden.yaml to sitesDir/<name>/.
func writeAppConfig(t *testing.T, sitesDir, name, domain, upstreamHost, upstreamPort string) {
	t.Helper()

	appDir := filepath.Join(sitesDir, name)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("creating %s dir: %v", name, err)
	}

	siteYAML := fmt.Sprintf(`profile: dev

server:
  host: "0.0.0.0"
  port: %s

upstream:
  host: "%s"
  port: %s

tls:
  enabled: true
  domain: "%s"
  provider: "self-signed"

auth:
  mode: "none"

security_headers:
  enabled: true

rate_limit:
  enabled: false
`, sidecarPort, upstreamHost, upstreamPort, domain)

	if err := os.WriteFile(filepath.Join(appDir, "vibewarden.yaml"), []byte(siteYAML), 0o644); err != nil {
		t.Fatalf("writing %s/vibewarden.yaml: %v", name, err)
	}
}

// startSidecarContainer creates and starts the VibeWarden sidecar container
// with the given config directory mounted. The sidecar runs in multi-site
// mode because the mounted directory contains a sites/ subdirectory.
func startSidecarContainer(ctx context.Context, t *testing.T, netName, configDir string) (testcontainers.Container, error) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image:        sidecarImage,
		ExposedPorts: []string{sidecarPort + "/tcp"},
		Networks:     []string{netName},
		NetworkAliases: map[string][]string{
			netName: {"vibewarden-sidecar"},
		},
		Cmd:        []string{"serve"},
		WorkingDir: "/config",
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Mounts = []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: configDir,
					Target: "/config",
				},
			}
		},
		WaitingFor: wait.ForLog("VibeWarden multi-site mode starting").
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating sidecar container: %w", err)
	}

	// Give Caddy a moment to finish TLS setup and start listening.
	time.Sleep(3 * time.Second)

	t.Logf("sidecar container started")
	return container, nil
}

// doRequest performs an HTTPS GET to the given URL with the specified Host
// header and returns the response body as a string. It fails the test on
// any error.
func doRequest(t *testing.T, client *http.Client, url, host string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = host

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request to %s (Host: %s): %v", url, host, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("request to %s (Host: %s): got status %d, want 200", url, host, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	return string(body)
}

// verifyTLS connects to the sidecar with a TLS handshake using the given
// ServerName and verifies that a certificate is served. We do not validate
// the certificate chain (self-signed), but we verify the TLS handshake
// completes and the peer certificate's CN or SAN includes the expected domain.
func verifyTLS(t *testing.T, host, port, serverName string) {
	t.Helper()

	conn, err := tls.Dial("tcp", host+":"+port, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true, //nolint:gosec // self-signed cert in test
	})
	if err != nil {
		t.Fatalf("TLS dial to %s:%s (SNI: %s): %v", host, port, serverName, err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("TLS handshake completed but no peer certificates received")
	}

	cert := state.PeerCertificates[0]

	// Check that the certificate covers the requested domain.
	domainFound := false
	if cert.Subject.CommonName == serverName {
		domainFound = true
	}
	for _, san := range cert.DNSNames {
		if san == serverName {
			domainFound = true
		}
	}

	if !domainFound {
		t.Errorf("TLS cert does not cover %q: CN=%q, SANs=%v",
			serverName, cert.Subject.CommonName, cert.DNSNames)
	}

	t.Logf("TLS verified for %s: CN=%q, SANs=%v", serverName, cert.Subject.CommonName, cert.DNSNames)
}

// verifySecurityHeaders sends a request with the given Host header and checks
// that standard security headers are present in the response.
func verifySecurityHeaders(t *testing.T, client *http.Client, baseURL, host string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Host = host

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request for security headers: %v", err)
	}
	defer resp.Body.Close()

	// Check for essential security headers set by the security_headers middleware.
	expectedHeaders := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
	}

	for _, h := range expectedHeaders {
		val := resp.Header.Get(h)
		if val == "" {
			t.Errorf("missing security header %q for Host %s", h, host)
		} else {
			t.Logf("Host %s: %s = %s", host, h, val)
		}
	}
}

// testErrorIsolation writes a broken vibewarden.yaml to sites/app3/ inside the
// sidecar's config directory, then verifies that app1 and app2 continue to
// serve correctly. This validates that a broken site config does not bring
// down healthy sites.
func testErrorIsolation(t *testing.T, _ context.Context, clientApp1, clientApp2 *http.Client, baseURL, configDir string) {
	t.Helper()

	// Write a broken site config.
	brokenDir := filepath.Join(configDir, "sites", "app3")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("creating app3 dir: %v", err)
	}

	brokenYAML := `this is not valid yaml: [[[`
	if err := os.WriteFile(filepath.Join(brokenDir, "vibewarden.yaml"), []byte(brokenYAML), 0o644); err != nil {
		t.Fatalf("writing broken app3 config: %v", err)
	}

	// The sidecar's file watcher should detect the new file. Give it time
	// to process the change (the watcher has a debounce interval).
	time.Sleep(5 * time.Second)

	// Verify app1 and app2 are still reachable.
	body1 := doRequest(t, clientApp1, baseURL+"/", "app1.local")
	if !strings.Contains(body1, `{"app":"app1"}`) {
		t.Errorf("app1 broken after bad app3 config: got %q", body1)
	}

	body2 := doRequest(t, clientApp2, baseURL+"/", "app2.local")
	if !strings.Contains(body2, `{"app":"app2"}`) {
		t.Errorf("app2 broken after bad app3 config: got %q", body2)
	}

	t.Log("error isolation verified: app1 and app2 serve correctly after broken app3 config")
}
