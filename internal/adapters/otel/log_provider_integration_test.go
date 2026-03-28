package otel_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestLogProvider_Integration_FullRoundtrip starts a fake OTLP endpoint, emits a
// log record via the otelslog bridge, forces a flush via Shutdown, and verifies
// that the endpoint received at least one OTLP logs request.
func TestLogProvider_Integration_FullRoundtrip(t *testing.T) {
	var received [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := readBody(r)
		if err != nil {
			t.Logf("reading body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		received = append(received, body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx := context.Background()
	p := oteladapter.NewLogProvider()
	cfg := ports.LogExportConfig{OTLPEnabled: true}
	if err := p.Init(ctx, "vibewarden-test", "0.0.1-test", srv.URL, cfg); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Emit a log record via the handler.
	h := p.Handler()
	if h == nil {
		t.Fatal("Handler() returned nil")
	}
	logger := slog.New(h)
	logger.Info("integration test event",
		slog.String("event_type", "auth.success"),
		slog.String("schema_version", "v1"),
		slog.String("ai_summary", "test event"),
	)

	// Shutdown flushes the batch processor.
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}

	if len(received) == 0 {
		t.Error("OTLP endpoint received no requests after Shutdown")
	}
}

// readBody reads and optionally decompresses the request body.
func readBody(r *http.Request) ([]byte, error) {
	var reader io.Reader = r.Body
	defer r.Body.Close()

	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		reader = gr
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
