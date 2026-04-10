// Package migrate provides the application service for database schema
// migration operations. It orchestrates the MigrationRunner port and adds
// structured logging around each operation.
package migrate

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates database migration operations via the MigrationRunner
// port. It adds logging and error context but contains no business logic.
type Service struct {
	runner ports.MigrationRunner
	logger *slog.Logger
}

// NewService creates a migration Service backed by the given MigrationRunner.
func NewService(runner ports.MigrationRunner, logger *slog.Logger) *Service {
	return &Service{
		runner: runner,
		logger: logger,
	}
}

// ApplyAll applies all pending database migrations. It logs the before/after
// version for observability.
func (s *Service) ApplyAll(ctx context.Context) error {
	before, err := s.runner.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status before apply: %w", err)
	}

	s.logger.Info("applying database migrations",
		slog.Int("current_version", before.CurrentVersion),
		slog.Bool("dirty", before.Dirty),
		slog.Int("pending_count", before.PendingCount),
	)

	if err := s.runner.Up(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	after, err := s.runner.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status after apply: %w", err)
	}

	s.logger.Info("database migrations applied",
		slog.Int("previous_version", before.CurrentVersion),
		slog.Int("current_version", after.CurrentVersion),
	)

	return nil
}

// Rollback rolls back the most recently applied migration.
func (s *Service) Rollback(ctx context.Context) error {
	before, err := s.runner.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status before rollback: %w", err)
	}

	s.logger.Info("rolling back last migration",
		slog.Int("current_version", before.CurrentVersion),
	)

	if err := s.runner.Down(ctx); err != nil {
		return fmt.Errorf("rolling back migration: %w", err)
	}

	after, err := s.runner.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status after rollback: %w", err)
	}

	s.logger.Info("migration rolled back",
		slog.Int("previous_version", before.CurrentVersion),
		slog.Int("current_version", after.CurrentVersion),
	)

	return nil
}

// PrintStatus writes the current migration status to w, including the version,
// dirty state, and pending migration count.
func (s *Service) PrintStatus(ctx context.Context, w io.Writer) error {
	v, err := s.runner.Status(ctx)
	if err != nil {
		return fmt.Errorf("reading migration status: %w", err)
	}

	if v.CurrentVersion == -1 {
		fmt.Fprintln(w, "No migrations applied yet.")
	} else {
		fmt.Fprintf(w, "Current version: %d\n", v.CurrentVersion)
	}

	if v.PendingCount > 0 {
		fmt.Fprintf(w, "Pending migrations: %d\n", v.PendingCount)
	}

	if v.Dirty {
		fmt.Fprintln(w, "WARNING: database is in a dirty state — manual intervention required.")
	}

	return nil
}

// Close releases resources held by the underlying runner.
func (s *Service) Close() error {
	return s.runner.Close()
}
