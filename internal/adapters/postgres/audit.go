// Package postgres provides PostgreSQL adapters for VibeWarden ports.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	// pgx stdlib driver — MIT license, already an indirect dependency.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// AuditAdapter implements ports.AuditLogger backed by a PostgreSQL database.
// Entries are written to the user_audit_log table (see migrations/).
// The table is append-only: this adapter never updates or deletes rows.
type AuditAdapter struct {
	db *sql.DB
}

// NewAuditAdapter opens a database connection using the given DSN and returns
// an AuditAdapter. The DSN must be a libpq-compatible connection string or URL
// (e.g. "postgres://user:pass@host:5432/db?sslmode=disable").
//
// The caller is responsible for calling Close when the adapter is no longer
// needed.
func NewAuditAdapter(dsn string) (*AuditAdapter, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}

	// Verify connectivity immediately so callers discover misconfiguration early.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		// Best-effort close; the ping error is the primary failure to surface.
		_ = db.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return &AuditAdapter{db: db}, nil
}

// Close releases the underlying database connection pool.
func (a *AuditAdapter) Close() error {
	return a.db.Close()
}

// RecordEntry inserts an audit entry into the user_audit_log table.
// If entry.Timestamp is zero, it is set to the current UTC time before
// insertion. The database also applies DEFAULT NOW() as a safety net.
//
// RecordEntry satisfies ports.AuditLogger.
func (a *AuditAdapter) RecordEntry(ctx context.Context, entry ports.AuditEntry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	var metadataJSON []byte
	if len(entry.Metadata) > 0 {
		b, err := json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("marshaling audit metadata: %w", err)
		}
		metadataJSON = b
	}

	// actor_id is nullable; pass nil when empty.
	var actorID *string
	if entry.ActorID != "" {
		actorID = &entry.ActorID
	}

	const q = `
		INSERT INTO user_audit_log (user_id, action, actor_id, timestamp, metadata)
		VALUES ($1::uuid, $2, $3, $4, $5)`

	_, err := a.db.ExecContext(ctx, q,
		entry.UserID,
		string(entry.Action),
		actorID,
		entry.Timestamp,
		nullableJSON(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("inserting audit entry: %w", err)
	}

	return nil
}

// nullableJSON returns a sql.NullString for JSONB columns.
// When b is nil or empty, the result is NULL.
func nullableJSON(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}
