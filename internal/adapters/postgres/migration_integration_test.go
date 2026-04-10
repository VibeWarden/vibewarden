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
)

func startPostgres(ctx context.Context, t *testing.T) string { //nolint:thelper // ctx first per Go convention
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

func TestMigrationAdapter_Integration_UpAndStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(ctx, t)

	adapter, err := postgres.NewMigrationAdapter(connStr, testMigrationFS())
	if err != nil {
		t.Fatalf("NewMigrationAdapter() error: %v", err)
	}
	defer adapter.Close() //nolint:errcheck

	v, err := adapter.Status(ctx)
	if err != nil {
		t.Fatalf("Status() before Up error: %v", err)
	}
	if v.CurrentVersion != -1 {
		t.Errorf("Status() before Up version = %d, want -1", v.CurrentVersion)
	}
	if v.PendingCount != 2 {
		t.Errorf("Status() before Up pending = %d, want 2", v.PendingCount)
	}

	if err := adapter.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	v, err = adapter.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after Up error: %v", err)
	}
	if v.CurrentVersion != 2 {
		t.Errorf("Status() after Up version = %d, want 2", v.CurrentVersion)
	}
	if v.Dirty {
		t.Error("Status() after Up dirty = true, want false")
	}
	if v.PendingCount != 0 {
		t.Errorf("Status() after Up pending = %d, want 0", v.PendingCount)
	}
}

func TestMigrationAdapter_Integration_UpIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(ctx, t)

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
	connStr := startPostgres(ctx, t)

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

	v, err := adapter.Status(ctx)
	if err != nil {
		t.Fatalf("Status() after Down error: %v", err)
	}
	if v.CurrentVersion != 1 {
		t.Errorf("Status() after Down version = %d, want 1", v.CurrentVersion)
	}
}

func TestMigrationAdapter_Integration_DownNoMigrations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	connStr := startPostgres(ctx, t)

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
