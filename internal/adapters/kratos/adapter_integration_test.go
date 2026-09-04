//go:build integration

// Package kratos contains integration tests that spin up real Kratos and
// Postgres containers via testcontainers-go.
package kratos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// kratosImage is the Kratos container image used across all integration tests.
const kratosImage = "oryd/kratos:v26.2.0"

// kratosNetworkDSN is the network-internal Postgres DSN used by Kratos
// containers when all services share the same Docker network.
const kratosNetworkDSN = "postgres://kratos:kratos@postgres:5432/kratos?sslmode=disable"

// kratosConfigYAML returns the minimal Kratos YAML configuration used by
// integration tests. pgDSN is the DSN reachable from inside the Docker
// network (i.e. the container-internal address, not the host-mapped port).
func kratosConfigYAML(pgDSN string) string {
	return `
version: v26.2.0

dsn: ` + pgDSN + `

serve:
  public:
    base_url: http://localhost:4433/
  admin:
    base_url: http://localhost:4434/

selfservice:
  default_browser_return_url: http://localhost:3000/
  allowed_return_urls:
    - http://localhost:3000

  methods:
    password:
      enabled: true

  flows:
    registration:
      enabled: true
      ui_url: http://localhost:3000/auth/registration
    login:
      ui_url: http://localhost:3000/auth/login
    logout:
      after:
        default_browser_return_url: http://localhost:3000/
    verification:
      enabled: false
    recovery:
      enabled: false

log:
  level: error

identity:
  default_schema_id: default
  schemas:
    - id: default
      url: base64://eyIkaWQiOiJodHRwczovL3NjaGVtYXMub3J5LnNoL3ByZXNldHMva3JhdG9zL3F1aWNrc3RhcnQvZW1haWwtcGFzc3dvcmQvaWRlbnRpdHkuc2NoZW1hLmpzb24iLCIkc2NoZW1hIjoiaHR0cDovL2pzb24tc2NoZW1hLm9yZy9kcmFmdC0wNy9zY2hlbWEjIiwidGl0bGUiOiJQZXJzb24iLCJ0eXBlIjoib2JqZWN0IiwicHJvcGVydGllcyI6eyJ0cmFpdHMiOnsidHlwZSI6Im9iamVjdCIsInByb3BlcnRpZXMiOnsiZW1haWwiOnsidHlwZSI6InN0cmluZyIsImZvcm1hdCI6ImVtYWlsIiwidGl0bGUiOiJFLU1haWwiLCJvcnkuc2gva3JhdG9zIjp7ImNyZWRlbnRpYWxzIjp7InBhc3N3b3JkIjp7ImlkZW50aWZpZXIiOnRydWV9fX19fX19fQ==

courier:
  smtp:
    connection_uri: smtp://test:test@localhost:25/?skip_ssl_verify=true
`
}

// testNetwork creates a named Docker network for a single test and registers
// cleanup. Returns the network name.
func testNetwork(ctx context.Context, t *testing.T) string {
	t.Helper()

	net, err := network.New(ctx)
	if err != nil {
		t.Fatalf("creating docker network: %v", err)
	}
	t.Cleanup(func() {
		if err := net.Remove(ctx); err != nil {
			t.Logf("removing docker network: %v", err)
		}
	})
	return net.Name
}

// startPostgres starts a Postgres container on the given Docker network with
// the alias "postgres" so that Kratos containers on the same network can reach
// it via hostname. Returns the network name (same as input) for convenience.
func startPostgres(ctx context.Context, t *testing.T, netName string) {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image: "postgres:17-alpine",
		Env: map[string]string{
			"POSTGRES_DB":       "kratos",
			"POSTGRES_USER":     "kratos",
			"POSTGRES_PASSWORD": "kratos",
		},
		Networks: []string{netName},
		NetworkAliases: map[string][]string{
			netName: {"postgres"},
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}

	pgContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("terminating postgres container: %v", err)
		}
	})
}

// migrateKratos runs a one-shot Kratos migrate container on the given Docker
// network and waits for it to exit. Must be called after startPostgres.
//
// v26.2.0 ships 338 SQL migrations. Running them inline during serve startup
// (which Kratos does in --dev mode when the schema is absent) exceeds the
// 120-second container readiness timeout. Running migrate as a dedicated step
// — matching the docker-compose.yml pattern — keeps the serve container fast.
func migrateKratos(ctx context.Context, t *testing.T, netName string) {
	t.Helper()

	cfg := kratosConfigYAML(kratosNetworkDSN)
	req := testcontainers.ContainerRequest{
		Image:    kratosImage,
		Networks: []string{netName},
		Env:      map[string]string{"DSN": kratosNetworkDSN},
		Cmd:      []string{"migrate", "sql", "-e", "--yes"},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(cfg),
				ContainerFilePath: "/etc/kratos/kratos.yml",
				FileMode:          0o644,
			},
		},
		WaitingFor: wait.ForExit().WithExitTimeout(180 * time.Second),
	}

	migrateContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting kratos migrate container: %v", err)
	}
	t.Cleanup(func() {
		if err := migrateContainer.Terminate(ctx); err != nil {
			t.Logf("terminating kratos migrate container: %v", err)
		}
	})
}

// kratosURLs holds the mapped public and admin base URLs for a running
// Kratos container. Defined here (not in admin_integration_test.go) so both
// test files share the same type.
type kratosURLs struct {
	public string
	admin  string
}

// startKratos starts a Kratos container on the given Docker network.
// Returns the host-accessible public and admin API base URLs. Runs database
// migrations via a separate one-shot container before starting the serve
// process so that startup is fast and the readiness check does not time out.
func startKratos(ctx context.Context, t *testing.T, netName string) kratosURLs {
	t.Helper()

	// Run migrations before serving. v26.2.0 has 338 migrations; without this
	// pre-migration step the serve startup exceeds the 120-second timeout.
	migrateKratos(ctx, t, netName)

	kratosConfig := kratosConfigYAML(kratosNetworkDSN)

	req := testcontainers.ContainerRequest{
		Image:        kratosImage,
		Networks:     []string{netName},
		ExposedPorts: []string{"4433/tcp", "4434/tcp"},
		Env:          map[string]string{},
		Cmd:          []string{"serve", "--config", "/etc/kratos/kratos.yml", "--dev", "--watch-courier"},
		Files: []testcontainers.ContainerFile{
			{
				Reader:            strings.NewReader(kratosConfig),
				ContainerFilePath: "/etc/kratos/kratos.yml",
				FileMode:          0o644,
			},
		},
		WaitingFor: wait.ForHTTP("/health/ready").
			WithPort("4433/tcp").
			WithStartupTimeout(120 * time.Second),
	}

	kratosContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting kratos container: %v", err)
	}
	t.Cleanup(func() {
		if err := kratosContainer.Terminate(ctx); err != nil {
			t.Logf("terminating kratos container: %v", err)
		}
	})

	host, err := kratosContainer.Host(ctx)
	if err != nil {
		t.Fatalf("getting kratos host: %v", err)
	}
	publicPort, err := kratosContainer.MappedPort(ctx, "4433")
	if err != nil {
		t.Fatalf("getting kratos public mapped port: %v", err)
	}
	adminPort, err := kratosContainer.MappedPort(ctx, "4434")
	if err != nil {
		t.Fatalf("getting kratos admin mapped port: %v", err)
	}

	return kratosURLs{
		public: fmt.Sprintf("http://%s:%s", host, publicPort.Port()),
		admin:  fmt.Sprintf("http://%s:%s", host, adminPort.Port()),
	}
}

// TestKratosAdapter_Integration_CheckSession_InvalidCookie verifies that the
// Kratos adapter returns ErrSessionInvalid for a bogus session cookie against
// a real Kratos instance.
func TestKratosAdapter_Integration_CheckSession_InvalidCookie(t *testing.T) {
	ctx := context.Background()

	netName := testNetwork(ctx, t)
	startPostgres(ctx, t, netName)
	urls := startKratos(ctx, t, netName)

	adapter := NewAdapter(urls.public, 10*time.Second, slog.Default())

	_, err := adapter.CheckSession(ctx, "ory_kratos_session=bogus-cookie-value")
	if err == nil {
		t.Fatal("expected error for invalid session cookie, got nil")
	}

	if !isSessionError(err) {
		t.Errorf("expected ErrSessionInvalid or ErrSessionNotFound, got: %v", err)
	}
}

// TestKratosAdapter_Integration_CheckSession_EmptyCookie verifies that the
// Kratos adapter returns ErrSessionNotFound for an empty session cookie.
func TestKratosAdapter_Integration_CheckSession_EmptyCookie(t *testing.T) {
	ctx := context.Background()

	netName := testNetwork(ctx, t)
	startPostgres(ctx, t, netName)
	urls := startKratos(ctx, t, netName)

	adapter := NewAdapter(urls.public, 10*time.Second, slog.Default())

	_, err := adapter.CheckSession(ctx, "")
	if err == nil {
		t.Fatal("expected ErrSessionNotFound for empty cookie, got nil")
	}

	if err != ports.ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got: %v", err)
	}
}

// TestKratosAdapter_Integration_CreateAndCheckSession creates a real Kratos
// identity and session via the admin API, then validates the session using
// CheckSession.
func TestKratosAdapter_Integration_CreateAndCheckSession(t *testing.T) {
	ctx := context.Background()

	netName := testNetwork(ctx, t)
	startPostgres(ctx, t, netName)
	urls := startKratos(ctx, t, netName)

	const (
		testEmail    = "test@example.com"
		testPassword = "test-password-123!"
	)

	// Create a test identity with known password credentials via the admin API.
	createTestIdentity(t, ctx, urls.admin, testEmail)

	// Obtain a session token via the self-service login flow.
	// POST /admin/sessions was removed in Kratos v25; login is the only path.
	sessionCookie := createTestSession(t, ctx, urls.public, testEmail, testPassword)

	// Now validate the session using the adapter.
	adapter := NewAdapter(urls.public, 10*time.Second, slog.Default())

	session, err := adapter.CheckSession(ctx, sessionCookie)
	if err != nil {
		t.Fatalf("CheckSession() unexpected error: %v", err)
	}

	if session == nil {
		t.Fatal("expected non-nil session")
	}
	if !session.Active {
		t.Error("expected session to be active")
	}
	if session.Identity.ID == "" {
		t.Error("expected non-empty identity ID")
	}
}

// isSessionError reports whether the error is one of the expected session errors.
func isSessionError(err error) bool {
	return err == ports.ErrSessionInvalid || err == ports.ErrSessionNotFound
}

// createTestIdentity creates a Kratos identity via the admin API and returns its ID.
func createTestIdentity(t *testing.T, ctx context.Context, adminURL, email string) string {
	t.Helper()

	body := fmt.Sprintf(`{
		"schema_id": "default",
		"traits": {"email": %q},
		"credentials": {
			"password": {
				"config": {"password": "test-password-123!"}
			}
		}
	}`, email)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		adminURL+"/admin/identities",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("building create identity request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("creating identity: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create identity status = %d, body = %s", resp.StatusCode, b)
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding create identity response: %v", err)
	}

	return result.ID
}

// createTestSession creates a Kratos session via the self-service login API flow
// and returns the session token as a cookie string ready for use in HTTP requests.
// The POST /admin/sessions endpoint was removed in Kratos v25; self-service login
// is now the only programmatic path to obtain a session token.
func createTestSession(t *testing.T, ctx context.Context, publicURL, email, password string) string {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Initialise an API login flow.
	initReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		publicURL+"/self-service/login/api", nil)
	if err != nil {
		t.Fatalf("building login flow init request: %v", err)
	}

	initResp, err := client.Do(initReq)
	if err != nil {
		t.Fatalf("initialising login flow: %v", err)
	}
	defer initResp.Body.Close()

	if initResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(initResp.Body)
		t.Fatalf("login flow init status = %d, body = %s", initResp.StatusCode, b)
	}

	var flowResult struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(initResp.Body).Decode(&flowResult); err != nil {
		t.Fatalf("decoding login flow response: %v", err)
	}

	// Step 2: Submit the login flow with password credentials.
	submitBody := fmt.Sprintf(`{"method":"password","identifier":%q,"password":%q}`,
		email, password)
	submitReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		publicURL+"/self-service/login?flow="+flowResult.ID,
		strings.NewReader(submitBody),
	)
	if err != nil {
		t.Fatalf("building login submit request: %v", err)
	}
	submitReq.Header.Set("Content-Type", "application/json")
	submitReq.Header.Set("Accept", "application/json")

	submitResp, err := client.Do(submitReq)
	if err != nil {
		t.Fatalf("submitting login flow: %v", err)
	}
	defer submitResp.Body.Close()

	if submitResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(submitResp.Body)
		t.Fatalf("login submit status = %d, body = %s", submitResp.StatusCode, b)
	}

	var sessionResult struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(submitResp.Body).Decode(&sessionResult); err != nil {
		t.Fatalf("decoding session response: %v", err)
	}

	if sessionResult.SessionToken == "" {
		t.Fatal("empty session_token in login response")
	}

	// Return the raw session token (ory_st_* prefix). CheckSession detects
	// this prefix and uses X-Session-Token instead of Cookie, which is
	// required for API-flow sessions in Kratos v25+.
	return sessionResult.SessionToken
}
