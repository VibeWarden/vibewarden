// Package migrate provides the application service for database schema
// migration operations. It orchestrates the MigrationRunner port and adds
// structured logging around each operation.
package migrate

import (
	"context"
	"fmt"
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

// Up applies all pending database migrations. It logs the before/after
// version for observability.
func (s *Service) Up(ctx context.Context) error {
	before, err := s.runner.Version(ctx)
	if err != nil {
		return fmt.Errorf("reading migration version before up: %w", err)
	}

	s.logger.Info("applying database migrations",
		slog.Int("current_version", before.Version),
		slog.Bool("dirty", before.Dirty),
	)

	if err := s.runner.Up(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	after, err := s.runner.Version(ctx)
	if err != nil {
		return fmt.Errorf("reading migration version after up: %w", err)
	}

	s.logger.Info("database migrations applied",
		slog.Int("previous_version", before.Version),
		slog.Int("current_version", after.Version),
	)

	return nil
}

// Down rolls back the most recently applied migration.
func (s *Service) Down(ctx context.Context) error {
	before, err := s.runner.Version(ctx)
	if err != nil {
		return fmt.Errorf("reading migration version before down: %w", err)
	}

	s.logger.Info("rolling back last migration",
		slog.Int("current_version", before.Version),
	)

	if err := s.runner.Down(ctx); err != nil {
		return fmt.Errorf("rolling back migration: %w", err)
	}

	after, err := s.runner.Version(ctx)
	if err != nil {
		return fmt.Errorf("reading migration version after down: %w", err)
	}

	s.logger.Info("migration rolled back",
		slog.Int("previous_version", before.Version),
		slog.Int("current_version", after.Version),
	)

	return nil
}

// Status returns the current migration version and dirty state.
func (s *Service) Status(ctx context.Context) (ports.MigrationVersion, error) {
	v, err := s.runner.Version(ctx)
	if err != nil {
		return ports.MigrationVersion{}, fmt.Errorf("reading migration status: %w", err)
	}
	return v, nil
}

// Close releases resources held by the underlying runner.
func (s *Service) Close() error {
	return s.runner.Close()
}
