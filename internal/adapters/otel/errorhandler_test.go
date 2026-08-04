package otel_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
)

// logRecord is the decoded shape of a single slog JSON line.
type logRecord struct {
	Level       string `json:"level"`
	Msg         string `json:"msg"`
	Error       string `json:"error"`
	RepeatCount int    `json:"repeat_count"`
}

// syncBuffer is a concurrency-safe io.Writer used as a log sink. The global
// OTel error handler is process-wide, so background exporters from other tests
// may write to it at any time.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogger returns a debug-level JSON logger writing into buf.
func captureLogger(buf *syncBuffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// decodeRecords parses every JSON line emitted into buf.
func decodeRecords(t *testing.T, buf *syncBuffer) []logRecord {
	t.Helper()
	var recs []logRecord
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec logRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("decoding log line %q: %v", line, err)
		}
		recs = append(recs, rec)
	}
	return recs
}

func TestErrorHandler_Handle(t *testing.T) {
	tests := []struct {
		name       string
		errs       []error
		wantLevels []string
		wantCounts []int
	}{
		{
			name:       "nil error is ignored",
			errs:       []error{nil},
			wantLevels: nil,
			wantCounts: nil,
		},
		{
			name:       "first error is warn",
			errs:       []error{errors.New("failed to upload metrics: no such host")},
			wantLevels: []string{"WARN"},
			wantCounts: []int{0},
		},
		{
			name: "repeated error is demoted to debug with counter",
			errs: []error{
				errors.New("failed to upload metrics: no such host"),
				errors.New("failed to upload metrics: no such host"),
				errors.New("failed to upload metrics: no such host"),
			},
			wantLevels: []string{"WARN", "DEBUG", "DEBUG"},
			wantCounts: []int{0, 1, 2},
		},
		{
			name: "different error resets dedup state",
			errs: []error{
				errors.New("failed to upload metrics: no such host"),
				errors.New("failed to upload metrics: no such host"),
				errors.New("failed to upload traces: connection refused"),
				errors.New("failed to upload traces: connection refused"),
			},
			wantLevels: []string{"WARN", "DEBUG", "WARN", "DEBUG"},
			wantCounts: []int{0, 1, 0, 1},
		},
		{
			name: "nil errors interleaved do not affect dedup state",
			errs: []error{
				errors.New("boom"),
				nil,
				errors.New("boom"),
			},
			wantLevels: []string{"WARN", "DEBUG"},
			wantCounts: []int{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf syncBuffer
			h := oteladapter.NewErrorHandler(captureLogger(&buf))
			for _, err := range tt.errs {
				h.Handle(err)
			}

			recs := decodeRecords(t, &buf)
			if len(recs) != len(tt.wantLevels) {
				t.Fatalf("got %d log records, want %d (output: %q)", len(recs), len(tt.wantLevels), buf.String())
			}
			for i, rec := range recs {
				if rec.Level != tt.wantLevels[i] {
					t.Errorf("record %d level = %q, want %q", i, rec.Level, tt.wantLevels[i])
				}
				if rec.RepeatCount != tt.wantCounts[i] {
					t.Errorf("record %d repeat_count = %d, want %d", i, rec.RepeatCount, tt.wantCounts[i])
				}
				if rec.Error == "" {
					t.Errorf("record %d has empty error attribute", i)
				}
			}
		})
	}
}

func TestErrorHandler_NilLoggerUsesDefault(t *testing.T) {
	var buf syncBuffer
	original := slog.Default()
	slog.SetDefault(captureLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(original) })

	oteladapter.NewErrorHandler(nil).Handle(errors.New("failed to upload metrics"))

	recs := decodeRecords(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("got %d log records, want 1 (output: %q)", len(recs), buf.String())
	}
	if recs[0].Error != "failed to upload metrics" {
		t.Errorf("error attribute = %q, want %q", recs[0].Error, "failed to upload metrics")
	}
}

func TestErrorHandler_ConcurrentHandleIsSafe(t *testing.T) {
	var buf syncBuffer
	h := oteladapter.NewErrorHandler(captureLogger(&buf))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Handle(errors.New("failed to upload metrics"))
		}()
	}
	wg.Wait()

	if got := len(decodeRecords(t, &buf)); got != 50 {
		t.Errorf("got %d log records, want 50", got)
	}
}

func TestInstallErrorHandler_RoutesGlobalOTelErrors(t *testing.T) {
	var buf syncBuffer
	oteladapter.InstallErrorHandler(captureLogger(&buf))
	t.Cleanup(func() { oteladapter.InstallErrorHandler(slog.New(slog.NewJSONHandler(io.Discard, nil))) })

	otel.Handle(errors.New("failed to upload metrics: dial tcp: lookup otel-collector: no such host"))

	recs := decodeRecords(t, &buf)
	if len(recs) != 1 {
		t.Fatalf("got %d log records, want 1 (output: %q)", len(recs), buf.String())
	}
	if recs[0].Level != "WARN" {
		t.Errorf("level = %q, want WARN", recs[0].Level)
	}
	if !strings.Contains(recs[0].Error, "otel-collector") {
		t.Errorf("error attribute = %q, want it to contain the underlying error", recs[0].Error)
	}
}
