package logprint_test

import (
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/vibewarden/vibewarden/internal/adapters/logprint"
)

func init() {
	// Disable colors in tests so assertions compare plain text.
	color.NoColor = true
}

func TestPrinter_Print_ValidJSON(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		opts        logprint.PrinterOptions
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "info event formatted correctly",
			line: `{"schema_version":"1","timestamp":"2026-03-26T10:30:45Z","level":"info","event_type":"auth.session_validated","ai_summary":"User authenticated","payload":{}}`,
			opts: logprint.PrinterOptions{},
			wantContain: []string{
				"2026-03-26 10:30:45",
				"INFO",
				"[auth.session_validated]",
				"User authenticated",
			},
		},
		{
			name: "warn level",
			line: `{"schema_version":"1","timestamp":"2026-03-26T10:30:46Z","level":"warn","event_type":"rate_limit.exceeded","ai_summary":"Rate limit hit","payload":{}}`,
			opts: logprint.PrinterOptions{},
			wantContain: []string{
				"WARN",
				"[rate_limit.exceeded]",
				"Rate limit hit",
			},
		},
		{
			name: "error level",
			line: `{"schema_version":"1","timestamp":"2026-03-26T10:30:47Z","level":"error","event_type":"proxy.upstream_error","ai_summary":"Upstream returned 502","payload":{}}`,
			opts: logprint.PrinterOptions{},
			wantContain: []string{
				"ERROR",
				"[proxy.upstream_error]",
				"Upstream returned 502",
			},
		},
		{
			name: "verbose mode includes payload",
			line: `{"schema_version":"1","timestamp":"2026-03-26T10:30:48Z","level":"info","event_type":"proxy.request","ai_summary":"Request proxied","payload":{"method":"GET","path":"/api/v1"}}`,
			opts: logprint.PrinterOptions{Verbose: true},
			wantContain: []string{
				"[proxy.request]",
				"Request proxied",
				"method",
				"GET",
				"/api/v1",
			},
		},
		{
			name: "verbose mode without payload shows no payload block",
			line: `{"schema_version":"1","timestamp":"2026-03-26T10:30:49Z","level":"info","event_type":"auth.login","ai_summary":"Login ok","payload":{}}`,
			opts: logprint.PrinterOptions{Verbose: true},
			wantContain: []string{
				"[auth.login]",
				"Login ok",
			},
			wantAbsent: []string{"{}"},
		},
		{
			name: "raw JSON mode passes line unchanged",
			line: `{"level":"info","event_type":"test","ai_summary":"raw","payload":{}}`,
			opts: logprint.PrinterOptions{RawJSON: true},
			wantContain: []string{
				`{"level":"info","event_type":"test","ai_summary":"raw","payload":{}}`,
			},
		},
		{
			name:        "filter matches event type prefix",
			line:        `{"schema_version":"1","timestamp":"2026-03-26T10:30:50Z","level":"info","event_type":"auth.session_validated","ai_summary":"Auth ok","payload":{}}`,
			opts:        logprint.PrinterOptions{Filter: "auth"},
			wantContain: []string{"[auth.session_validated]"},
		},
		{
			name: "filter skips non-matching event type",
			line: `{"schema_version":"1","timestamp":"2026-03-26T10:30:51Z","level":"info","event_type":"proxy.request","ai_summary":"Request proxied","payload":{}}`,
			opts: logprint.PrinterOptions{Filter: "auth"},
			// Output should be empty — event is filtered out.
			wantAbsent: []string{"proxy.request", "Request proxied"},
		},
		{
			name: "docker compose prefix stripped before JSON parse",
			line: `vibewarden-1  | {"schema_version":"1","timestamp":"2026-03-26T10:30:52Z","level":"info","event_type":"auth.login","ai_summary":"Login via compose","payload":{}}`,
			opts: logprint.PrinterOptions{},
			wantContain: []string{
				"[auth.login]",
				"Login via compose",
			},
		},
		{
			name:        "filter is case-insensitive",
			line:        `{"schema_version":"1","timestamp":"2026-03-26T10:30:53Z","level":"info","event_type":"Auth.Session","ai_summary":"Auth match","payload":{}}`,
			opts:        logprint.PrinterOptions{Filter: "AUTH"},
			wantContain: []string{"[Auth.Session]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := logprint.NewPrinter(tt.opts)
			var buf strings.Builder
			if err := p.Print(tt.line, &buf); err != nil {
				t.Fatalf("Print() error = %v", err)
			}
			out := buf.String()
			for _, want := range tt.wantContain {
				if !strings.Contains(out, want) {
					t.Errorf("output %q does not contain %q", out, want)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out, absent) {
					t.Errorf("output %q should not contain %q", out, absent)
				}
			}
		})
	}
}

func TestPrinter_Print_NonJSON(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantContain string
	}{
		{
			name:        "plain text line forwarded verbatim",
			line:        "Attaching to vibewarden-1",
			wantContain: "Attaching to vibewarden-1",
		},
		{
			name:        "empty line forwarded",
			line:        "",
			wantContain: "",
		},
		{
			name:        "docker compose startup banner forwarded",
			line:        " Container vibewarden-1  Started",
			wantContain: "Container vibewarden-1",
		},
		{
			name:        "malformed JSON forwarded verbatim",
			line:        `{"level":"info","missing_close":`,
			wantContain: `{"level":"info","missing_close":`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := logprint.NewPrinter(logprint.PrinterOptions{})
			var buf strings.Builder
			if err := p.Print(tt.line, &buf); err != nil {
				t.Fatalf("Print() error = %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, tt.wantContain) {
				t.Errorf("output %q does not contain %q", out, tt.wantContain)
			}
		})
	}
}

func TestPrinter_ColorAssignment(t *testing.T) {
	// Color assignment is verified by checking that INFO/WARN/ERROR labels
	// appear for the respective levels. Colors are disabled in tests so the
	// label text itself is the differentiator.
	tests := []struct {
		level     string
		wantLabel string
	}{
		{"info", "INFO"},
		{"warn", "WARN"},
		{"warning", "WARN"},
		{"error", "ERROR"},
		{"", "INFO"}, // unknown defaults to INFO
	}

	for _, tt := range tests {
		t.Run("level_"+tt.level, func(t *testing.T) {
			line := `{"schema_version":"1","timestamp":"2026-03-26T10:00:00Z","level":"` +
				tt.level + `","event_type":"test.event","ai_summary":"test","payload":{}}`
			p := logprint.NewPrinter(logprint.PrinterOptions{})
			var buf strings.Builder
			if err := p.Print(line, &buf); err != nil {
				t.Fatalf("Print() error = %v", err)
			}
			if !strings.Contains(buf.String(), tt.wantLabel) {
				t.Errorf("output %q does not contain level label %q", buf.String(), tt.wantLabel)
			}
		})
	}
}
