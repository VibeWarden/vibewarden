// Package ports defines the interfaces (inbound and outbound) for the
// hexagonal architecture.
//
// This file defines the MigrationRunner port for database schema migrations.
package ports

import "context"

// MigrationVersion represents the current state of database migrations.
type MigrationVersion struct {
	// Version is the current migration version number. A value of -1
	// indicates that no migrations have been applied (the NilVersion
	// sentinel from golang-migrate).
	Version int

	// Dirty is true when a migration failed mid-execution and the
	// database is in an inconsistent state requiring manual intervention.
	Dirty bool
}

// MigrationRunner abstracts database schema migration operations.
// Implementations are expected to use advisory locks to ensure only one
// runner executes migrations at a time, making it safe for concurrent
// startup of multiple VibeWarden instances.
type MigrationRunner interface {
	// Up applies all pending migrations. It returns nil when all
	// migrations have been applied or when there are no pending
	// migrations.
	Up(ctx context.Context) error

	// Down rolls back the most recently applied migration. It returns an
	// error if no migrations have been applied.
	Down(ctx context.Context) error

	// Version returns the current migration version and dirty state. A
	// version of -1 means no migrations have been applied yet.
	Version(ctx context.Context) (MigrationVersion, error)

	// Close releases any resources held by the runner (e.g. database
	// connections). It must be called when the runner is no longer needed.
	Close() error
}
