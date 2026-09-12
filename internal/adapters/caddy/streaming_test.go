package caddy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// sseChunk is the first thing a text/event-stream upstream typically writes.
const sseChunk = "retry: 3000\n\n"

// flushViaFlusher flushes w through the http.Flusher interface, the way most
// application handlers and Go's httputil.ReverseProxy do.
func flushViaFlusher(w http.ResponseWriter) error {
	f, ok := w.(http.Flusher)
	if !ok {
		return errors.New("response writer does not implement http.Flusher")
	}
	f.Flush()
	return nil
}

// flushViaController flushes w through http.ResponseController, the way Caddy's
// reverse proxy does. It only works when every wrapper in the chain implements
// Unwrap() http.ResponseWriter.
func flushViaController(w http.ResponseWriter) error {
	return http.NewResponseController(w).Flush()
}

// TestCaddyHandlers_StreamingResponseReachesClientBeforeHandlerReturns is the
// behavioural guard for #1526 on the Caddy-module side: the circuit breaker and
// timeout handlers wrap the ResponseWriter, and a wrapper that hides
// http.Flusher turns an SSE response into one the client only sees on close.
func TestCaddyHandlers_StreamingResponseReachesClientBeforeHandlerReturns(t *testing.T) {
	handlers := []struct {
		name string
		wrap func(next caddyhttp.Handler) http.Handler
	}{
		{
			name: "circuit breaker",
			wrap: func(next caddyhttp.Handler) http.Handler {
				h := &CircuitBreakerHandler{cb: &fakeCB{}}
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := h.ServeHTTP(w, r, next); err != nil {
						t.Errorf("circuit breaker ServeHTTP: %v", err)
					}
				})
			},
		},
		{
			name: "timeout",
			wrap: func(next caddyhttp.Handler) http.Handler {
				h := &TimeoutHandler{Config: TimeoutHandlerConfig{TimeoutSeconds: 30}}
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := h.ServeHTTP(w, r, next); err != nil {
						t.Errorf("timeout ServeHTTP: %v", err)
					}
				})
			},
		},
		{
			name: "circuit breaker -> timeout",
			wrap: func(next caddyhttp.Handler) http.Handler {
				timeout := &TimeoutHandler{Config: TimeoutHandlerConfig{TimeoutSeconds: 30}}
				breaker := &CircuitBreakerHandler{cb: &fakeCB{}}
				inner := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
					return timeout.ServeHTTP(w, r, next)
				})
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := breaker.ServeHTTP(w, r, inner); err != nil {
						t.Errorf("chain ServeHTTP: %v", err)
					}
				})
			},
		},
	}

	strategies := []struct {
		name  string
		flush func(http.ResponseWriter) error
	}{
		{"http.Flusher assertion", flushViaFlusher},
		{"http.ResponseController", flushViaController},
	}

	for _, h := range handlers {
		for _, s := range strategies {
			t.Run(h.name+"/"+s.name, func(t *testing.T) {
				assertCaddyHandlerStreams(t, h.wrap, s.flush)
			})
		}
	}
}

// assertCaddyHandlerStreams serves wrap(streaming upstream) over a real HTTP
// server and fails unless the first chunk reaches the client while the upstream
// is still holding the response open.
func assertCaddyHandlerStreams(t *testing.T, wrap func(caddyhttp.Handler) http.Handler, flush func(http.ResponseWriter) error) {
	t.Helper()

	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })

	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Type", "text/event-stream")
		if _, err := io.WriteString(w, sseChunk); err != nil {
			return err
		}
		if err := flush(w); err != nil {
			t.Errorf("flushing: %v", err)
			return nil
		}
		<-release
		return nil
	})

	srv := httptest.NewServer(wrap(upstream))
	defer srv.Close()
	defer releaseOnce()

	type result struct {
		data string
		err  error
	}
	got := make(chan result, 1)

	go func() {
		resp, err := http.Get(srv.URL) //nolint:noctx // no deadline: the upstream blocks on purpose
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
		t.Fatal("no bytes reached the client while the upstream was still streaming: a response writer wrapper swallowed the flush")
	}
}
