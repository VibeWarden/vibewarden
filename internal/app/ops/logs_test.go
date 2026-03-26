package ops_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// fakePrinter records all calls to Print so tests can inspect them.
type fakePrinter struct {
	lines []string
	err   error
}

func (f *fakePrinter) Print(line string, out io.Writer) error {
	if f.err != nil {
		return f.err
	}
	f.lines = append(f.lines, line)
	_, err := io.WriteString(out, line+"\n")
	return err
}

func TestLogsService_Run_Stdin(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLines  []string
		wantOutput []string
	}{
		{
			name:  "single JSON line forwarded to printer",
			input: `{"level":"info","event_type":"auth.login","ai_summary":"ok"}` + "\n",
			wantLines: []string{
				`{"level":"info","event_type":"auth.login","ai_summary":"ok"}`,
			},
		},
		{
			name:  "multiple lines all forwarded",
			input: "line one\nline two\nline three\n",
			wantLines: []string{
				"line one",
				"line two",
				"line three",
			},
		},
		{
			name:       "empty input produces no lines",
			input:      "",
			wantLines:  nil,
			wantOutput: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			printer := &fakePrinter{}
			svc := ops.NewLogsService(printer)
			var buf strings.Builder

			err := svc.Run(context.Background(), ops.LogsOptions{
				Stdin: strings.NewReader(tt.input),
			}, &buf)

			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if len(printer.lines) != len(tt.wantLines) {
				t.Fatalf("got %d lines, want %d: %v", len(printer.lines), len(tt.wantLines), printer.lines)
			}
			for i, want := range tt.wantLines {
				if printer.lines[i] != want {
					t.Errorf("line[%d]: got %q, want %q", i, printer.lines[i], want)
				}
			}
		})
	}
}

func TestLogsService_Run_ContextCancellation(t *testing.T) {
	// A cancelled context must not cause Run to return an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	printer := &fakePrinter{}
	svc := ops.NewLogsService(printer)
	var buf strings.Builder

	// Even with a long input stream the function must return nil because the
	// context is already cancelled.
	longInput := strings.Repeat("line\n", 1000)

	err := svc.Run(ctx, ops.LogsOptions{Stdin: strings.NewReader(longInput)}, &buf)
	if err != nil {
		t.Errorf("Run() with cancelled context returned error = %v, want nil", err)
	}
}

func TestLogsService_Run_PrinterError(t *testing.T) {
	// When the printer returns an error, Run must propagate it.
	printer := &fakePrinter{err: io.ErrClosedPipe}
	svc := ops.NewLogsService(printer)
	var buf strings.Builder

	err := svc.Run(context.Background(), ops.LogsOptions{
		Stdin: strings.NewReader("some line\n"),
	}, &buf)

	if err == nil {
		t.Fatal("Run() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "printing log line") {
		t.Errorf("error %q does not contain expected context", err.Error())
	}
}
