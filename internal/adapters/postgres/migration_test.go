package postgres_test

import (
	"testing"
	"testing/fstest"

	"github.com/vibewarden/vibewarden/internal/adapters/postgres"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Verify MigrationAdapter implements ports.MigrationRunner at compile time.
var _ ports.MigrationRunner = (*postgres.MigrationAdapter)(nil)

func TestNewMigrationAdapter_InvalidURL(t *testing.T) {
	fs := fstest.MapFS{
		"1_init.up.sql":   {Data: []byte("CREATE TABLE t (id int);")},
		"1_init.down.sql": {Data: []byte("DROP TABLE t;")},
	}

	_, err := postgres.NewMigrationAdapter("postgres://invalid:5432/nonexistent?connect_timeout=1", fs)
	if err == nil {
		t.Error("NewMigrationAdapter() expected error for unreachable database, got nil")
	}
}

func TestNewMigrationAdapter_EmptyFS(t *testing.T) {
	fs := fstest.MapFS{}

	_, err := postgres.NewMigrationAdapter("postgres://localhost:5432/test", fs)
	if err == nil {
		t.Error("NewMigrationAdapter() expected error for empty migration FS, got nil")
	}
}
