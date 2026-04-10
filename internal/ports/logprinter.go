// Package ports defines the interfaces (ports) for VibeWarden's hexagonal architecture.
package ports

import "io"

// LogPrinter formats and writes a single structured log line to an output writer.
// Implementations receive the raw line as read from the log stream and are
// responsible for JSON parsing, colorisation, filtering, and fallback handling.
type LogPrinter interface {
	// Print writes a formatted representation of line to out.
	// Non-JSON lines must be forwarded verbatim without returning an error.
	Print(line string, out io.Writer) error
}
