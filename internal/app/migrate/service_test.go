package migrate_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/migrate"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeMigrationRunner is a simple test double for ports.MigrationRunner.
type fakeMigrationRunner struct {
	status     ports.MigrationStatus
	upCalled   bool
	downCalled bool
	closed     bool

	upErr     error
	downErr   error
	statusErr error
	closeErr  error

	// statusAfterUp allows simulating a version bump after Up.
	statusAfterUp *ports.MigrationStatus
	// statusAfterDown allows simulating a version change after Down.
	statusAfterDown *ports.MigrationStatus

	statusCallCount int
}

func (f *fakeMigrationRunner) Up(_ context.Context) error {
	f.upCalled = true
	if f.upErr != nil {
		return f.upErr
	}
	if f.statusAfterUp != nil {
		f.status = *f.statusAfterUp
	}
	return nil
}

func (f *fakeMigrationRunner) Down(_ context.Context) error {
	f.downCalled = true
	if f.downErr != nil {
		return f.downErr
	}
	if f.statusAfterDown != nil {
		f.status = *f.statusAfterDown
	}
	return nil
}

func (f *fakeMigrationRunner) Status(_ context.Context) (ports.MigrationStatus, error) {
	f.statusCallCount++
	if f.statusErr != nil {
		return ports.MigrationStatus{}, f.statusErr
	}
	return f.status, nil
}

func (f *fakeMigrationRunner) Close() error {
	f.closed = true
	return f.closeErr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestService_ApplyAll(t *testing.T) {
	tests := []struct {
		name    string
		runner  *fakeMigrationRunner
		wantErr bool
	}{
		{
			name: "successful migration",
			runner: &fakeMigrationRunner{
				status:        ports.MigrationStatus{CurrentVersion: -1, PendingCount: 2},
				statusAfterUp: &ports.MigrationStatus{CurrentVersion: 20260326120000, PendingCount: 0},
			},
			wantErr: false,
		},
		{
			name: "up returns error",
			runner: &fakeMigrationRunner{
				status: ports.MigrationStatus{CurrentVersion: -1},
				upErr:  errors.New("connection refused"),
			},
			wantErr: true,
		},
		{
			name: "status read error before apply",
			runner: &fakeMigrationRunner{
				statusErr: errors.New("timeout"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := migrate.NewService(tt.runner, testLogger())
			err := svc.ApplyAll(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("ApplyAll() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !tt.runner.upCalled {
				t.Error("ApplyAll() did not call runner.Up()")
			}
		})
	}
}

func TestService_Rollback(t *testing.T) {
	tests := []struct {
		name    string
		runner  *fakeMigrationRunner
		wantErr bool
	}{
		{
			name: "successful rollback",
			runner: &fakeMigrationRunner{
				status:          ports.MigrationStatus{CurrentVersion: 20260326120000},
				statusAfterDown: &ports.MigrationStatus{CurrentVersion: -1},
			},
			wantErr: false,
		},
		{
			name: "down returns error",
			runner: &fakeMigrationRunner{
				status:  ports.MigrationStatus{CurrentVersion: 20260326120000},
				downErr: errors.New("no migrations to roll back"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := migrate.NewService(tt.runner, testLogger())
			err := svc.Rollback(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Rollback() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !tt.runner.downCalled {
				t.Error("Rollback() did not call runner.Down()")
			}
		})
	}
}

func TestService_PrintStatus(t *testing.T) {
	tests := []struct {
		name           string
		runner         *fakeMigrationRunner
		wantErr        bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "no migrations applied",
			runner: &fakeMigrationRunner{
				status: ports.MigrationStatus{CurrentVersion: -1, Dirty: false, PendingCount: 2},
			},
			wantErr:      false,
			wantContains: []string{"No migrations applied yet.", "Pending migrations: 2"},
		},
		{
			name: "migration applied and clean",
			runner: &fakeMigrationRunner{
				status: ports.MigrationStatus{CurrentVersion: 20260326120000, Dirty: false, PendingCount: 0},
			},
			wantErr:        false,
			wantContains:   []string{"Current version: 20260326120000"},
			wantNotContain: []string{"WARNING", "Pending"},
		},
		{
			name: "dirty state",
			runner: &fakeMigrationRunner{
				status: ports.MigrationStatus{CurrentVersion: 20260326120000, Dirty: true},
			},
			wantErr:      false,
			wantContains: []string{"Current version: 20260326120000", "WARNING"},
		},
		{
			name: "status read error",
			runner: &fakeMigrationRunner{
				statusErr: errors.New("connection lost"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := migrate.NewService(tt.runner, testLogger())
			var buf bytes.Buffer
			err := svc.PrintStatus(context.Background(), &buf)
			if (err != nil) != tt.wantErr {
				t.Errorf("PrintStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				output := buf.String()
				for _, want := range tt.wantContains {
					if !strings.Contains(output, want) {
						t.Errorf("PrintStatus() output missing %q, got: %q", want, output)
					}
				}
				for _, notWant := range tt.wantNotContain {
					if strings.Contains(output, notWant) {
						t.Errorf("PrintStatus() output should not contain %q, got: %q", notWant, output)
					}
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
