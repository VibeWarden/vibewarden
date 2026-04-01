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
	m          *migrate.Migrate
	migrations fs.FS
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

	return &MigrationAdapter{m: m, migrations: migrations}, nil
}

// Up applies all pending migrations. It returns nil when all migrations have
// been applied or when there are no pending migrations.
//
// The context parameter is accepted for interface conformance but is not
// used by the underlying golang-migrate library, which does not support
// context-based cancellation.
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
//
// The context parameter is accepted for interface conformance but is not
// used by the underlying golang-migrate library, which does not support
// context-based cancellation.
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

// Status returns the current migration status including version, dirty state,
// and the number of pending migrations.
//
// The context parameter is accepted for interface conformance but is not
// used by the underlying golang-migrate library, which does not support
// context-based cancellation.
func (a *MigrationAdapter) Status(_ context.Context) (ports.MigrationStatus, error) {
	version, dirty, err := a.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		pending, countErr := a.countMigrationFiles()
		if countErr != nil {
			return ports.MigrationStatus{}, fmt.Errorf("counting migration files: %w", countErr)
		}
		return ports.MigrationStatus{CurrentVersion: -1, Dirty: false, PendingCount: pending}, nil
	}
	if err != nil {
		return ports.MigrationStatus{}, fmt.Errorf("reading migration version: %w", err)
	}

	pending, countErr := a.countPending(int(version))
	if countErr != nil {
		return ports.MigrationStatus{}, fmt.Errorf("counting pending migrations: %w", countErr)
	}

	return ports.MigrationStatus{
		CurrentVersion: int(version),
		Dirty:          dirty,
		PendingCount:   pending,
	}, nil
}

// countMigrationFiles counts the total number of unique migration versions
// (up files) in the embedded filesystem.
func (a *MigrationAdapter) countMigrationFiles() (int, error) {
	entries, err := fs.Glob(a.migrations, "*.up.sql")
	if err != nil {
		return 0, fmt.Errorf("globbing migration files: %w", err)
	}
	return len(entries), nil
}

// countPending counts migrations with version numbers greater than current.
func (a *MigrationAdapter) countPending(currentVersion int) (int, error) {
	total, err := a.countMigrationFiles()
	if err != nil {
		return 0, err
	}
	// golang-migrate uses sequential integer versions starting at 1.
	// Pending = total files - current version (assuming no gaps).
	pending := total - currentVersion
	if pending < 0 {
		pending = 0
	}
	return pending, nil
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
