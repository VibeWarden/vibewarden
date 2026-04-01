package migrate_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/migrate"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeMigrationRunner is a simple test double for ports.MigrationRunner.
type fakeMigrationRunner struct {
	version    ports.MigrationVersion
	upCalled   bool
	downCalled bool
	closed     bool

	upErr      error
	downErr    error
	versionErr error
	closeErr   error

	// versionAfterUp allows simulating a version bump after Up.
	versionAfterUp *ports.MigrationVersion
	// versionAfterDown allows simulating a version change after Down.
	versionAfterDown *ports.MigrationVersion

	versionCallCount int
}

func (f *fakeMigrationRunner) Up(_ context.Context) error {
	f.upCalled = true
	if f.upErr != nil {
		return f.upErr
	}
	if f.versionAfterUp != nil {
		f.version = *f.versionAfterUp
	}
	return nil
}

func (f *fakeMigrationRunner) Down(_ context.Context) error {
	f.downCalled = true
	if f.downErr != nil {
		return f.downErr
	}
	if f.versionAfterDown != nil {
		f.version = *f.versionAfterDown
	}
	return nil
}

func (f *fakeMigrationRunner) Version(_ context.Context) (ports.MigrationVersion, error) {
	f.versionCallCount++
	if f.versionErr != nil {
		return ports.MigrationVersion{}, f.versionErr
	}
	return f.version, nil
}

func (f *fakeMigrationRunner) Close() error {
	f.closed = true
	return f.closeErr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestService_Up(t *testing.T) {
	tests := []struct {
		name    string
		runner  *fakeMigrationRunner
		wantErr bool
	}{
		{
			name: "successful migration",
			runner: &fakeMigrationRunner{
				version:        ports.MigrationVersion{Version: -1},
				versionAfterUp: &ports.MigrationVersion{Version: 20260326120000},
			},
			wantErr: false,
		},
		{
			name: "up returns error",
			runner: &fakeMigrationRunner{
				version: ports.MigrationVersion{Version: -1},
				upErr:   errors.New("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "version read error before up",
			runner: &fakeMigrationRunner{
				versionErr: errors.New("timeout"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := migrate.NewService(tt.runner, testLogger())
			err := svc.Up(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Up() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !tt.runner.upCalled {
				t.Error("Up() did not call runner.Up()")
			}
		})
	}
}

func TestService_Down(t *testing.T) {
	tests := []struct {
		name    string
		runner  *fakeMigrationRunner
		wantErr bool
	}{
		{
			name: "successful rollback",
			runner: &fakeMigrationRunner{
				version:          ports.MigrationVersion{Version: 20260326120000},
				versionAfterDown: &ports.MigrationVersion{Version: -1},
			},
			wantErr: false,
		},
		{
			name: "down returns error",
			runner: &fakeMigrationRunner{
				version: ports.MigrationVersion{Version: 20260326120000},
				downErr: errors.New("no migrations to roll back"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := migrate.NewService(tt.runner, testLogger())
			err := svc.Down(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Down() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !tt.runner.downCalled {
				t.Error("Down() did not call runner.Down()")
			}
		})
	}
}

func TestService_Status(t *testing.T) {
	tests := []struct {
		name        string
		runner      *fakeMigrationRunner
		wantVersion int
		wantDirty   bool
		wantErr     bool
	}{
		{
			name: "no migrations applied",
			runner: &fakeMigrationRunner{
				version: ports.MigrationVersion{Version: -1, Dirty: false},
			},
			wantVersion: -1,
			wantDirty:   false,
			wantErr:     false,
		},
		{
			name: "migration applied and clean",
			runner: &fakeMigrationRunner{
				version: ports.MigrationVersion{Version: 20260326120000, Dirty: false},
			},
			wantVersion: 20260326120000,
			wantDirty:   false,
			wantErr:     false,
		},
		{
			name: "dirty state",
			runner: &fakeMigrationRunner{
				version: ports.MigrationVersion{Version: 20260326120000, Dirty: true},
			},
			wantVersion: 20260326120000,
			wantDirty:   true,
			wantErr:     false,
		},
		{
			name: "version read error",
			runner: &fakeMigrationRunner{
				versionErr: errors.New("connection lost"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := migrate.NewService(tt.runner, testLogger())
			v, err := svc.Status(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Status() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if v.Version != tt.wantVersion {
					t.Errorf("Status() version = %d, want %d", v.Version, tt.wantVersion)
				}
				if v.Dirty != tt.wantDirty {
					t.Errorf("Status() dirty = %v, want %v", v.Dirty, tt.wantDirty)
				}
			}
		})
	}
}

func TestService_Close(t *testing.T) {
	runner := &fakeMigrationRunner{}
	svc := migrate.NewService(runner, testLogger())

	if err := svc.Close(); err != nil {
		t.Errorf("Close() unexpected error: %v", err)
	}
	if !runner.closed {
		t.Error("Close() did not call runner.Close()")
	}
}

func TestService_Close_Error(t *testing.T) {
	runner := &fakeMigrationRunner{closeErr: errors.New("close failed")}
	svc := migrate.NewService(runner, testLogger())

	err := svc.Close()
	if err == nil {
		t.Error("Close() expected error, got nil")
	}
}
