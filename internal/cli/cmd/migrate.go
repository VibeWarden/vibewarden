package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	pgadapter "github.com/vibewarden/vibewarden/internal/adapters/postgres"
	migratesvc "github.com/vibewarden/vibewarden/internal/app/migrate"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/migrations"
)

// NewMigrateCmd creates the `vibewarden migrate` command group and its
// subcommands (up, down, status). When invoked without a subcommand it
// applies all pending migrations (equivalent to `vibewarden migrate up`).
func NewMigrateCmd() *cobra.Command {
	var configPath string

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database schema migrations",
		Long: `Apply, roll back, or inspect the state of database schema migrations.

When invoked without a subcommand, applies all pending migrations (same as "migrate up").

Requires a configured database URL in vibewarden.yaml (database.url or database.external_url).

Examples:
  vibewarden migrate
  vibewarden migrate up
  vibewarden migrate down
  vibewarden migrate status`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrateUp(cmd, configPath)
		},
	}

	migrateCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to vibewarden.yaml config file")

	migrateCmd.AddCommand(newMigrateUpCmd(&configPath))
	migrateCmd.AddCommand(newMigrateDownCmd(&configPath))
	migrateCmd.AddCommand(newMigrateStatusCmd(&configPath))

	return migrateCmd
}

func newMigrateUpCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrateUp(cmd, *configPath)
		},
	}
}

func newMigrateDownCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Roll back the most recently applied migration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrateDown(cmd, *configPath)
		},
	}
}

func newMigrateStatusCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current migration version and state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrateStatus(cmd, *configPath)
		},
	}
}

func buildMigrateService(configPath string) (*migratesvc.Service, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	dbURL := cfg.Database.ResolveURL()
	if dbURL == "" {
		return nil, fmt.Errorf("no database URL configured; set database.url or database.external_url in vibewarden.yaml")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	runner, err := pgadapter.NewMigrationAdapter(dbURL, migrations.FS)
	if err != nil {
		return nil, fmt.Errorf("creating migration runner: %w", err)
	}

	return migratesvc.NewService(runner, logger), nil
}

func runMigrateUp(cmd *cobra.Command, configPath string) error {
	svc, err := buildMigrateService(configPath)
	if err != nil {
		return err
	}
	defer svc.Close() //nolint:errcheck

	if err := svc.ApplyAll(cmd.Context()); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Migrations applied successfully.")
	return nil
}

func runMigrateDown(cmd *cobra.Command, configPath string) error {
	svc, err := buildMigrateService(configPath)
	if err != nil {
		return err
	}
	defer svc.Close() //nolint:errcheck

	if err := svc.Rollback(cmd.Context()); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Last migration rolled back successfully.")
	return nil
}

func runMigrateStatus(cmd *cobra.Command, configPath string) error {
	svc, err := buildMigrateService(configPath)
	if err != nil {
		return err
	}
	defer svc.Close() //nolint:errcheck

	if err := svc.PrintStatus(cmd.Context(), cmd.OutOrStdout()); err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}

	return nil
}
