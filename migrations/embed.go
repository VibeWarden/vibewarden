// Package migrations embeds the SQL migration files so they can be shipped
// inside the VibeWarden binary without requiring filesystem access at runtime.
package migrations

import "embed"

// FS contains all *.sql migration files embedded at compile time.
// It is used by the postgres migration adapter via the io/fs source driver.
//
//go:embed *.sql
var FS embed.FS
