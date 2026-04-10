//go:build integration

// Package kratos contains integration tests for the AdminAdapter.
// These tests spin up real Kratos and Postgres containers via testcontainers-go
// and exercise the four UserAdmin operations against a live Kratos instance.
package kratos

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// kratosURLs holds the mapped public and admin base URLs for a running
// Kratos container.
type kratosURLs struct {
	public string
	admin  string
}

// startKratosWithAdmin starts a Kratos container connected to the given
// Postgres DSN and returns both the public and admin API base URLs.
// Port mapping is resolved from the container runtime so the URLs are always
// correct regardless of host port assignment.
func startKratosWithAdmin(ctx context.Context, t *testing.T, pgDSN string) kratosURLs {
	t.Helper()

	kratosConfig := `
version: v1.3.0

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
      enabled: true
      ui_url: http://localhost:3000/auth/recovery

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

	req := testcontainers.ContainerRequest{
		Image:        "oryd/kratos:v1.3.0",
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

	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("starting kratos container: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Terminate(ctx); err != nil {
			t.Logf("terminating kratos container: %v", err)
		}
	})

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("getting kratos host: %v", err)
	}

	publicPort, err := c.MappedPort(ctx, "4433")
	if err != nil {
		t.Fatalf("getting kratos public mapped port: %v", err)
	}

	adminPort, err := c.MappedPort(ctx, "4434")
	if err != nil {
		t.Fatalf("getting kratos admin mapped port: %v", err)
	}

	return kratosURLs{
		public: fmt.Sprintf("http://%s:%s", host, publicPort.Port()),
		admin:  fmt.Sprintf("http://%s:%s", host, adminPort.Port()),
	}
}

// TestAdminAdapter_Integration_ListUsers_ReturnsCreatedUsers verifies that
// ListUsers returns identities created via InviteUser.
func TestAdminAdapter_Integration_ListUsers_ReturnsCreatedUsers(t *testing.T) {
	ctx := context.Background()

	pgDSN := startPostgres(ctx, t)
	urls := startKratosWithAdmin(ctx, t, pgDSN)

	adapter := NewAdminAdapter(urls.admin, 10*time.Second, slog.Default())

	// Invite two users so the list is non-empty.
	_, err := adapter.InviteUser(ctx, "list-alpha@example.com")
	if err != nil {
		t.Fatalf("InviteUser(alpha) error = %v", err)
	}
	_, err = adapter.InviteUser(ctx, "list-beta@example.com")
	if err != nil {
		t.Fatalf("InviteUser(beta) error = %v", err)
	}

	result, err := adapter.ListUsers(ctx, ports.Pagination{Page: 1, PerPage: 25})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(result.Users) < 2 {
		t.Errorf("expected at least 2 users, got %d", len(result.Users))
	}

	// All returned users must have an ID and email.
	for _, u := range result.Users {
		if u.ID == "" {
			t.Errorf("user with empty ID: %+v", u)
		}
		if u.Email == "" {
			t.Errorf("user %q has empty email", u.ID)
		}
	}
}

// TestAdminAdapter_Integration_GetUser_ByID verifies that GetUser returns the
// correct user when given a valid UUID returned by InviteUser.
func TestAdminAdapter_Integration_GetUser_ByID(t *testing.T) {
	ctx := context.Background()

	pgDSN := startPostgres(ctx, t)
	urls := startKratosWithAdmin(ctx, t, pgDSN)

	adapter := NewAdminAdapter(urls.admin, 10*time.Second, slog.Default())

	invite, err := adapter.InviteUser(ctx, "getbyid@example.com")
	if err != nil {
		t.Fatalf("InviteUser() error = %v", err)
	}

	got, err := adapter.GetUser(ctx, invite.User.ID)
	if err != nil {
		t.Fatalf("GetUser(%q) error = %v", invite.User.ID, err)
	}

	if got.ID != invite.User.ID {
		t.Errorf("ID = %q, want %q", got.ID, invite.User.ID)
	}
	if got.Email != "getbyid@example.com" {
		t.Errorf("Email = %q, want %q", got.Email, "getbyid@example.com")
	}
	if got.Status != user.StatusActive {
		t.Errorf("Status = %q, want %q", got.Status, user.StatusActive)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, expected a non-zero timestamp")
	}
}

// TestAdminAdapter_Integration_InviteUser_CreatesIdentityInKratos verifies
// that InviteUser creates a real identity retrievable via GetUser.
func TestAdminAdapter_Integration_InviteUser_CreatesIdentityInKratos(t *testing.T) {
	ctx := context.Background()

	pgDSN := startPostgres(ctx, t)
	urls := startKratosWithAdmin(ctx, t, pgDSN)

	adapter := NewAdminAdapter(urls.admin, 10*time.Second, slog.Default())

	const email = "invited@example.com"
	result, err := adapter.InviteUser(ctx, email)
	if err != nil {
		t.Fatalf("InviteUser(%q) error = %v", email, err)
	}

	if result.User.ID == "" {
		t.Error("InviteUser returned user with empty ID")
	}
	if result.User.Email != email {
		t.Errorf("User.Email = %q, want %q", result.User.Email, email)
	}
	if result.User.Status != user.StatusActive {
		t.Errorf("User.Status = %q, want %q", result.User.Status, user.StatusActive)
	}

	// Confirm the identity is actually present in Kratos.
	fetched, err := adapter.GetUser(ctx, result.User.ID)
	if err != nil {
		t.Fatalf("GetUser(%q) after InviteUser error = %v", result.User.ID, err)
	}
	if fetched.Email != email {
		t.Errorf("fetched email = %q, want %q", fetched.Email, email)
	}
}

// TestAdminAdapter_Integration_DeactivateUser_SetsStateInactive verifies that
// DeactivateUser changes the identity state to inactive in Kratos.
func TestAdminAdapter_Integration_DeactivateUser_SetsStateInactive(t *testing.T) {
	ctx := context.Background()

	pgDSN := startPostgres(ctx, t)
	urls := startKratosWithAdmin(ctx, t, pgDSN)

	adapter := NewAdminAdapter(urls.admin, 10*time.Second, slog.Default())

	invite, err := adapter.InviteUser(ctx, "deactivate-me@example.com")
	if err != nil {
		t.Fatalf("InviteUser() error = %v", err)
	}

	// Confirm the user is active before deactivation.
	before, err := adapter.GetUser(ctx, invite.User.ID)
	if err != nil {
		t.Fatalf("GetUser() before deactivate error = %v", err)
	}
	if before.Status != user.StatusActive {
		t.Fatalf("expected active before deactivation, got %q", before.Status)
	}

	// Deactivate.
	if err := adapter.DeactivateUser(ctx, invite.User.ID); err != nil {
		t.Fatalf("DeactivateUser(%q) error = %v", invite.User.ID, err)
	}

	// Confirm the state is now inactive.
	after, err := adapter.GetUser(ctx, invite.User.ID)
	if err != nil {
		t.Fatalf("GetUser() after deactivate error = %v", err)
	}
	if after.Status != user.StatusInactive {
		t.Errorf("Status after deactivation = %q, want %q", after.Status, user.StatusInactive)
	}
}

// TestAdminAdapter_Integration_GetUser_NonExistentID_ReturnsErrUserNotFound
// verifies that GetUser returns ErrUserNotFound for a UUID that has never been
// registered in Kratos.
func TestAdminAdapter_Integration_GetUser_NonExistentID_ReturnsErrUserNotFound(t *testing.T) {
	ctx := context.Background()

	pgDSN := startPostgres(ctx, t)
	urls := startKratosWithAdmin(ctx, t, pgDSN)

	adapter := NewAdminAdapter(urls.admin, 10*time.Second, slog.Default())

	// A syntactically valid UUID that has never been inserted.
	const phantom = "00000000-0000-0000-0000-000000000001"

	_, err := adapter.GetUser(ctx, phantom)
	if err == nil {
		t.Fatal("expected ErrUserNotFound, got nil")
	}
	if err != ports.ErrUserNotFound {
		t.Errorf("GetUser(phantom) error = %v, want ErrUserNotFound", err)
	}
}
