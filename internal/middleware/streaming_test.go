package middleware

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
)

// sseChunk is the first thing a text/event-stream handler typically writes.
const sseChunk = "retry: 3000\n\n"

// flushStrategy is one of the two ways a handler asks for a flush. Both must
// work through every wrapper: applications commonly type-assert to
// http.Flusher, while Caddy's reverse proxy uses http.ResponseController,
// which walks the Unwrap chain.
type flushStrategy struct {
	name  string
	flush func(w http.ResponseWriter) error
}

var flushStrategies = []flushStrategy{
	{
		name: "http.Flusher assertion",
		flush: func(w http.ResponseWriter) error {
			f, ok := w.(http.Flusher)
			if !ok {
				return errors.New("response writer does not implement http.Flusher")
			}
			f.Flush()
			return nil
		},
	},
	{
		name: "http.ResponseController",
		flush: func(w http.ResponseWriter) error {
			return http.NewResponseController(w).Flush()
		},
	},
}

// TestMiddlewareWrappers_StreamingResponseReachesClientBeforeHandlerReturns is
// the behavioural guard for #1526: an SSE handler flushes its first chunk and
// then blocks. If any wrapper in the chain swallows the flush, the bytes sit in
// the net/http buffer until the handler returns and the read below times out.
//
// Asserting on the wrapper's method set alone would pass while the proxy
// silently buffers, so this drives a real HTTP server and a real client.
func TestMiddlewareWrappers_StreamingResponseReachesClientBeforeHandlerReturns(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	identity := func(p string) string { return p }

	chains := []struct {
		name string
		wrap func(http.Handler) http.Handler
	}{
		{
			name: "metrics",
			wrap: MetricsMiddleware(&fakeMetricsCollector{}, identity),
		},
		{
			name: "tracing",
			wrap: TracingMiddleware(&oteladapter.MockTracer{}, identity, nil),
		},
		{
			name: "access log",
			wrap: AccessLogMiddleware(discard, true, false),
		},
		{
			name: "full chain (tracing -> metrics -> access log)",
			wrap: func(next http.Handler) http.Handler {
				return TracingMiddleware(&oteladapter.MockTracer{}, identity, nil)(
					MetricsMiddleware(&fakeMetricsCollector{}, identity)(
						AccessLogMiddleware(discard, true, false)(next),
					),
				)
			},
		},
	}

	for _, chain := range chains {
		for _, strategy := range flushStrategies {
			t.Run(chain.name+"/"+strategy.name, func(t *testing.T) {
				assertStreamsBeforeHandlerReturns(t, func(release <-chan struct{}) http.Handler {
					return chain.wrap(streamingTestHandler(t, strategy.flush, release))
				})
			})
		}
	}
}

// streamingTestHandler returns a handler that writes one SSE chunk, flushes it
// with the given strategy, and then blocks until release is closed.
func streamingTestHandler(t *testing.T, flush func(http.ResponseWriter) error, release <-chan struct{}) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, sseChunk); err != nil {
			t.Errorf("writing chunk: %v", err)
			return
		}
		if err := flush(w); err != nil {
			t.Errorf("flushing: %v", err)
			return
		}
		<-release
	})
}

// assertStreamsBeforeHandlerReturns serves the handler built by newHandler over
// a real HTTP server and fails unless the first chunk reaches the client while
// the handler is still blocked on the release channel.
func assertStreamsBeforeHandlerReturns(t *testing.T, newHandler func(release <-chan struct{}) http.Handler) {
	t.Helper()

	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })

	srv := httptest.NewServer(newHandler(release))
	defer srv.Close()
	defer releaseOnce()

	type result struct {
		data string
		err  error
	}
	got := make(chan result, 1)

	go func() {
		resp, err := http.Get(srv.URL) //nolint:noctx // no deadline: the handler blocks on purpose
		if err != nil {
			got <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		buf := make([]byte, len(sseChunk))
		if _, err := io.ReadFull(resp.Body, buf); err != nil {
			got <- result{err: err}
			return
		}
		got <- result{data: string(buf)}
	}()

	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("reading streamed chunk: %v", r.err)
		}
		if r.data != sseChunk {
			t.Errorf("streamed chunk = %q, want %q", r.data, sseChunk)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no bytes reached the client while the handler was still streaming: a response writer wrapper swallowed the flush")
	}
}
