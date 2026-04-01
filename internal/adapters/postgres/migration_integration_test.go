package postgres_test

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/vibewarden/vibewarden/internal/adapters/postgres"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func startPostgres(t *testing.T, ctx context.Context) string {
	t.Helper()

	container, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("terminating postgres container: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}
	return connStr
}

func testMigrationFS() fstest.MapFS {
	return fstest.MapFS{
		"1_create_test.up.sql":   {Data: []byte("CREATE TABLE test_table (id serial PRIMARY KEY, name text);")},
		"1_create_test.down.sql": {Data: []byte("DROP TABLE IF EXISTS test_table;")},
		"2_add_column.up.sql":    {Data: []byte("ALTER TABLE test_table ADD COLUMN email text;")},
		"2_add_column.down.sql":  {Data: []byte("ALTER TABLE test_table DROP COLUMN email;")},
	}
}

func TestMigrationAdapter_Integration_UpAndVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(t, ctx)

	adapter, err := postgres.NewMigrationAdapter(connStr, testMigrationFS())
	if err != nil {
		t.Fatalf("NewMigrationAdapter() error: %v", err)
	}
	defer adapter.Close() //nolint:errcheck

	v, err := adapter.Version(ctx)
	if err != nil {
		t.Fatalf("Version() before Up error: %v", err)
	}
	if v.Version != -1 {
		t.Errorf("Version() before Up = %d, want -1", v.Version)
	}

	if err := adapter.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	v, err = adapter.Version(ctx)
	if err != nil {
		t.Fatalf("Version() after Up error: %v", err)
	}
	if v.Version != 2 {
		t.Errorf("Version() after Up = %d, want 2", v.Version)
	}
	if v.Dirty {
		t.Error("Version() after Up dirty = true, want false")
	}
}

func TestMigrationAdapter_Integration_UpIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(t, ctx)

	adapter, err := postgres.NewMigrationAdapter(connStr, testMigrationFS())
	if err != nil {
		t.Fatalf("NewMigrationAdapter() error: %v", err)
	}
	defer adapter.Close() //nolint:errcheck

	if err := adapter.Up(ctx); err != nil {
		t.Fatalf("first Up() error: %v", err)
	}
	if err := adapter.Up(ctx); err != nil {
		t.Fatalf("second Up() should be idempotent, got error: %v", err)
	}
}

func TestMigrationAdapter_Integration_Down(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(t, ctx)

	adapter, err := postgres.NewMigrationAdapter(connStr, testMigrationFS())
	if err != nil {
		t.Fatalf("NewMigrationAdapter() error: %v", err)
	}
	defer adapter.Close() //nolint:errcheck

	if err := adapter.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	if err := adapter.Down(ctx); err != nil {
		t.Fatalf("Down() error: %v", err)
	}

	v, err := adapter.Version(ctx)
	if err != nil {
		t.Fatalf("Version() after Down error: %v", err)
	}
	if v.Version != 1 {
		t.Errorf("Version() after Down = %d, want 1", v.Version)
	}
}

func TestMigrationAdapter_Integration_DownNoMigrations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(t, ctx)

	adapter, err := postgres.NewMigrationAdapter(connStr, testMigrationFS())
	if err != nil {
		t.Fatalf("NewMigrationAdapter() error: %v", err)
	}
	defer adapter.Close() //nolint:errcheck

	err = adapter.Down(ctx)
	if err == nil {
		t.Error("Down() with no migrations applied expected error, got nil")
	}
}

// Verify the real adapter satisfies the port interface.
func TestMigrationAdapter_ImplementsPort(t *testing.T) {
	var _ ports.MigrationRunner = (*postgres.MigrationAdapter)(nil)
}
