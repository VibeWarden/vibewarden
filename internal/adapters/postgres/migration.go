// Package postgres provides adapters for PostgreSQL-backed persistence,
// including database schema migration.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	// Register the postgres database driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// MigrationAdapter implements ports.MigrationRunner using golang-migrate with
// an embedded filesystem of SQL migration files and a PostgreSQL database.
type MigrationAdapter struct {
	m *migrate.Migrate
}

// NewMigrationAdapter creates a MigrationAdapter that applies the SQL files
// from migrations to the PostgreSQL database at databaseURL.
//
// The adapter acquires a PostgreSQL advisory lock (via golang-migrate's
// built-in locking) to ensure safe concurrent startup of multiple VibeWarden
// instances.
func NewMigrationAdapter(databaseURL string, migrations fs.FS) (*MigrationAdapter, error) {
	source, err := iofs.New(migrations, ".")
	if err != nil {
		return nil, fmt.Errorf("creating iofs migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("creating migrate instance: %w", err)
	}

	return &MigrationAdapter{m: m}, nil
}

// Up applies all pending migrations. It returns nil when all migrations have
// been applied or when there are no pending migrations.
func (a *MigrationAdapter) Up(_ context.Context) error {
	err := a.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// Down rolls back the most recently applied migration. It returns an error if
// no migrations have been applied.
func (a *MigrationAdapter) Down(_ context.Context) error {
	err := a.m.Steps(-1)
	if errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("no migrations to roll back")
	}
	if err != nil {
		return fmt.Errorf("rolling back migration: %w", err)
	}
	return nil
}

// Version returns the current migration version and dirty state.
func (a *MigrationAdapter) Version(_ context.Context) (ports.MigrationVersion, error) {
	version, dirty, err := a.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return ports.MigrationVersion{Version: -1, Dirty: false}, nil
	}
	if err != nil {
		return ports.MigrationVersion{}, fmt.Errorf("reading migration version: %w", err)
	}
	return ports.MigrationVersion{
		Version: int(version),
		Dirty:   dirty,
	}, nil
}

// Close releases resources held by the migration runner.
func (a *MigrationAdapter) Close() error {
	sourceErr, dbErr := a.m.Close()
	if sourceErr != nil {
		return fmt.Errorf("closing migration source: %w", sourceErr)
	}
	if dbErr != nil {
		return fmt.Errorf("closing migration database: %w", dbErr)
	}
	return nil
}
