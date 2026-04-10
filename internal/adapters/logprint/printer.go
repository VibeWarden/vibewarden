// Package logprint provides a colorised terminal printer for VibeWarden's
// structured JSON log events (schema v1).
package logprint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fatih/color"
)

// Event represents the v1 VibeWarden log schema.
// Fields are a subset of the full schema; unknown fields are silently ignored.
type Event struct {
	// SchemaVersion identifies the log schema version (e.g. "1").
	SchemaVersion string `json:"schema_version"`
	// Timestamp is the ISO 8601 event time.
	Timestamp string `json:"timestamp"`
	// Level is the severity level: "info", "warn", or "error".
	Level string `json:"level"`
	// EventType is the dot-namespaced event identifier (e.g. "auth.session_validated").
	EventType string `json:"event_type"`
	// AISummary is the human/AI-readable one-line description of the event.
	AISummary string `json:"ai_summary"`
	// Payload holds optional event-specific fields.
	Payload map[string]any `json:"payload,omitempty"`
}

// PrinterOptions configures the behaviour of Printer.
type PrinterOptions struct {
	// Verbose enables printing the JSON payload after the summary line.
	Verbose bool
	// Filter, when non-empty, restricts output to events whose EventType
	// contains the filter string (case-insensitive prefix match).
	Filter string
	// RawJSON passes each line through without any formatting or filtering.
	RawJSON bool
}

// Printer is a colorised terminal printer for VibeWarden v1 log events.
// It implements ports.LogPrinter.
type Printer struct {
	opts PrinterOptions

	// colour functions — set at construction time so tests can override.
	fmtTimestamp func(...any) string
	fmtLevel     func(level string) string
	fmtEventType func(...any) string
	fmtSummary   func(...any) string
	fmtPayload   func(...any) string
}

// NewPrinter creates a Printer with the supplied options.
// Colour output follows fatih/color's global NoColor flag, which is
// automatically set when stdout is not a TTY.
func NewPrinter(opts PrinterOptions) *Printer {
	dim := color.New(color.Faint).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	white := color.New(color.FgWhite).SprintFunc()
	faint := color.New(color.Faint).SprintFunc()

	return &Printer{
		opts:         opts,
		fmtTimestamp: dim,
		fmtLevel:     buildLevelFormatter(),
		fmtEventType: cyan,
		fmtSummary:   white,
		fmtPayload:   faint,
	}
}

// buildLevelFormatter returns a function that maps a level string to a
// colourised label padded to five characters.
func buildLevelFormatter() func(string) string {
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	return func(level string) string {
		switch strings.ToLower(level) {
		case "warn", "warning":
			return yellow("WARN ")
		case "error":
			return red("ERROR")
		default:
			return green("INFO ")
		}
	}
}

// Print formats line and writes it to out.
// Lines that cannot be parsed as JSON are forwarded verbatim.
// The method never returns an error for malformed input; it only propagates
// I/O errors from out.
func (p *Printer) Print(line string, out io.Writer) error {
	trimmed := strings.TrimSpace(line)

	// In raw-JSON mode emit the line unchanged.
	if p.opts.RawJSON {
		_, err := fmt.Fprintln(out, line)
		return err
	}

	// Locate the first '{' to skip docker compose service-name prefixes such as
	// "vibewarden-1  | {…}".
	jsonStart := strings.Index(trimmed, "{")
	if jsonStart == -1 {
		// Not JSON — emit verbatim.
		_, err := fmt.Fprintln(out, line)
		return err
	}

	jsonPart := trimmed[jsonStart:]
	var ev Event
	if err := json.Unmarshal([]byte(jsonPart), &ev); err != nil {
		// Malformed JSON — emit verbatim.
		_, err2 := fmt.Fprintln(out, line)
		return err2
	}

	// Apply filter.
	if p.opts.Filter != "" &&
		!strings.HasPrefix(strings.ToLower(ev.EventType), strings.ToLower(p.opts.Filter)) {
		return nil
	}

	ts := formatTimestamp(ev.Timestamp)
	level := p.fmtLevel(ev.Level)
	eventType := p.fmtEventType("[" + ev.EventType + "]")
	summary := p.fmtSummary(ev.AISummary)

	_, err := fmt.Fprintf(out, "%s %s %s %s\n",
		p.fmtTimestamp(ts),
		level,
		eventType,
		summary,
	)
	if err != nil {
		return err
	}

	if p.opts.Verbose && len(ev.Payload) > 0 {
		encoded, encErr := json.MarshalIndent(ev.Payload, "    ", "  ")
		if encErr == nil {
			_, err = fmt.Fprintf(out, "    %s\n", p.fmtPayload(string(encoded)))
		}
	}

	return err
}

// formatTimestamp parses an ISO 8601 timestamp and returns it in
// "2006-01-02 15:04:05" local format. Returns the raw string on parse failure.
func formatTimestamp(raw string) string {
	if raw == "" {
		return "                   " // placeholder width
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// Try without nanoseconds.
		t, err = time.Parse("2006-01-02T15:04:05Z", raw)
		if err != nil {
			return raw
		}
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}
