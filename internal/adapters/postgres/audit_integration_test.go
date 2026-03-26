//go:build integration

// Package postgres contains integration tests for the PostgreSQL audit adapter.
// These tests spin up a real Postgres container via testcontainers-go.
package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// startTestPostgres starts a Postgres container, applies the audit log schema,
// and returns both the AuditAdapter and a cleanup function.
func startTestPostgres(ctx context.Context, t *testing.T) (*AuditAdapter, *sql.DB) {
	t.Helper()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("vibewarden"),
		tcpostgres.WithUsername("vibewarden"),
		tcpostgres.WithPassword("vibewarden"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("terminating postgres container: %v", err)
		}
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("getting connection string: %v", err)
	}

	adapter, err := NewAuditAdapter(dsn)
	if err != nil {
		t.Fatalf("NewAuditAdapter: %v", err)
	}
	t.Cleanup(func() { _ = adapter.Close() })

	// Apply the schema manually so the test is self-contained.
	applySchema(ctx, t, adapter.db)

	return adapter, adapter.db
}

// applySchema creates the user_audit_log table in the test database.
func applySchema(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()

	const ddl = `
CREATE TABLE user_audit_log (
    id        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID         NOT NULL,
    action    VARCHAR(50)  NOT NULL,
    actor_id  VARCHAR(255),
    timestamp TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    metadata  JSONB
);
CREATE INDEX idx_audit_user_id   ON user_audit_log(user_id);
CREATE INDEX idx_audit_timestamp ON user_audit_log(timestamp);
CREATE INDEX idx_audit_actor_id  ON user_audit_log(actor_id) WHERE actor_id IS NOT NULL;
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("applying schema: %v", err)
	}
}

func TestAuditAdapter_RecordEntry(t *testing.T) {
	ctx := context.Background()
	adapter, db := startTestPostgres(ctx, t)

	tests := []struct {
		name     string
		entry    ports.AuditEntry
		wantErr  bool
		checkRow func(t *testing.T)
	}{
		{
			name: "creates entry with actor and metadata",
			entry: ports.AuditEntry{
				UserID:    "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
				Action:    ports.AuditActionUserCreated,
				ActorID:   "admin-1",
				Timestamp: time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC),
				Metadata: map[string]any{
					"email": "alice@example.com",
				},
			},
			checkRow: func(t *testing.T) {
				t.Helper()
				var action, actorID string
				var metadata []byte
				err := db.QueryRowContext(ctx,
					`SELECT action, actor_id, metadata FROM user_audit_log
					 WHERE user_id = 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'`,
				).Scan(&action, &actorID, &metadata)
				if err != nil {
					t.Fatalf("querying row: %v", err)
				}
				if action != string(ports.AuditActionUserCreated) {
					t.Errorf("action = %q, want %q", action, ports.AuditActionUserCreated)
				}
				if actorID != "admin-1" {
					t.Errorf("actor_id = %q, want %q", actorID, "admin-1")
				}
				if string(metadata) == "" {
					t.Error("metadata should not be empty")
				}
			},
		},
		{
			name: "creates entry with nil actor and no metadata",
			entry: ports.AuditEntry{
				UserID: "b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22",
				Action: ports.AuditActionUserDeactivated,
			},
			checkRow: func(t *testing.T) {
				t.Helper()
				var count int
				err := db.QueryRowContext(ctx,
					`SELECT COUNT(*) FROM user_audit_log
					 WHERE user_id = 'b1eebc99-9c0b-4ef8-bb6d-6bb9bd380a22'`,
				).Scan(&count)
				if err != nil {
					t.Fatalf("querying count: %v", err)
				}
				if count != 1 {
					t.Errorf("expected 1 row, got %d", count)
				}
			},
		},
		{
			name: "timestamp defaults to now when zero",
			entry: ports.AuditEntry{
				UserID: "c2eebc99-9c0b-4ef8-bb6d-6bb9bd380a33",
				Action: ports.AuditActionUserCreated,
				// Timestamp intentionally left zero.
			},
			checkRow: func(t *testing.T) {
				t.Helper()
				var ts time.Time
				err := db.QueryRowContext(ctx,
					`SELECT timestamp FROM user_audit_log
					 WHERE user_id = 'c2eebc99-9c0b-4ef8-bb6d-6bb9bd380a33'`,
				).Scan(&ts)
				if err != nil {
					t.Fatalf("querying row: %v", err)
				}
				if ts.IsZero() {
					t.Error("timestamp should not be zero")
				}
				if time.Since(ts) > 10*time.Second {
					t.Errorf("timestamp %v is too far in the past", ts)
				}
			},
		},
		{
			name: "invalid user_id UUID returns error",
			entry: ports.AuditEntry{
				UserID: "not-a-uuid",
				Action: ports.AuditActionUserCreated,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := adapter.RecordEntry(ctx, tt.entry)
			if (err != nil) != tt.wantErr {
				t.Fatalf("RecordEntry() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.checkRow != nil {
				tt.checkRow(t)
			}
		})
	}
}
