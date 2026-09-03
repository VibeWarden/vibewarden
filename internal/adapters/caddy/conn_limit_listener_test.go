package caddy

import (
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
)

// echoServer runs an accept loop on ln, echoing every byte back on each
// accepted connection and closing the connection when the peer goes away.
// Closing the server side is what releases the slot in the connection cap.
func echoServer(t *testing.T, ln net.Listener) {
	t.Helper()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
}

// dialEcho opens a connection, sends a probe and reads the echo back.
// It returns an error if the connection is refused or does not echo in time.
func dialEcho(addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		_ = conn.Close()
		return nil, err
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if string(buf) != "ping" {
		_ = conn.Close()
		return nil, errors.New("unexpected echo payload: " + string(buf))
	}
	return conn, nil
}

// eventually polls cond until it returns true or the deadline passes.
func eventually(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestConnLimitListener_RefusesOverCap is the load-bearing behavioural test for
// ADR-110. It exercises a real TCP listener rather than asserting on the shape
// of the emitted Caddy JSON, because a config-shape assertion passes green
// against a silent no-op.
//
// It pins four properties:
//   - connections up to the cap are served normally;
//   - the connection past the cap is refused promptly (EOF/RST), NOT left
//     hanging — a blocking implementation such as netutil.LimitListener fails
//     here with a deadline-exceeded error instead of EOF;
//   - closing an admitted connection releases its slot;
//   - the active counter returns to zero, including after a refusal.
func TestConnLimitListener_RefusesOverCap(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	limiter := &ConnLimitListener{MaxConnections: 2}
	if err := limiter.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	wrapped := limiter.WrapListener(ln)
	echoServer(t, wrapped)
	addr := wrapped.Addr().String()

	// Two connections fit under the cap and must work normally.
	c1, err := dialEcho(addr)
	if err != nil {
		t.Fatalf("first connection (under cap) failed: %v", err)
	}
	defer func() { _ = c1.Close() }()

	c2, err := dialEcho(addr)
	if err != nil {
		t.Fatalf("second connection (at cap) failed: %v", err)
	}
	defer func() { _ = c2.Close() }()

	if got := limiter.active.Load(); got != 2 {
		t.Fatalf("active connections = %d, want 2", got)
	}

	// The third connection is over the cap. The TCP handshake still completes
	// (the kernel backlog accepts it), so the refusal is observed as a prompt
	// EOF/RST on read, not as a dial error.
	c3, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dialing over-cap connection: %v", err)
	}
	defer func() { _ = c3.Close() }()

	if err := c3.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	_, _ = c3.Write([]byte("ping"))
	_, readErr := c3.Read(make([]byte, 4))
	if readErr == nil {
		t.Fatal("over-cap connection was served; want refusal")
	}
	if errors.Is(readErr, os.ErrDeadlineExceeded) {
		t.Fatalf("over-cap connection hung until the deadline (%v); "+
			"connections past the cap must be refused, not accepted and left waiting", readErr)
	}

	// The client observes the refusal as soon as conn.Close() lands, which is
	// before the accept loop records it, so poll rather than reading the
	// counter straight after the failed read.
	if !eventually(func() bool { return limiter.refused.Load() == 1 }, 2*time.Second) {
		t.Errorf("refused count = %d, want 1", limiter.refused.Load())
	}
	// The refused path must roll back the increment it made.
	if got := limiter.active.Load(); got != 2 {
		t.Errorf("active connections after refusal = %d, want 2 (refused path leaked a slot)", got)
	}

	// Closing an admitted connection frees a slot; the server side closes
	// asynchronously once its io.Copy returns, hence the poll.
	if err := c1.Close(); err != nil {
		t.Fatalf("closing first connection: %v", err)
	}
	if !eventually(func() bool { return limiter.active.Load() == 1 }, 2*time.Second) {
		t.Fatalf("active connections = %d after close, want 1", limiter.active.Load())
	}

	c4, err := dialEcho(addr)
	if err != nil {
		t.Fatalf("connection after a slot was released failed: %v", err)
	}
	defer func() { _ = c4.Close() }()

	// No counter leak once everything is closed.
	_ = c2.Close()
	_ = c4.Close()
	if !eventually(func() bool { return limiter.active.Load() == 0 }, 2*time.Second) {
		t.Errorf("active connections = %d after all closed, want 0", limiter.active.Load())
	}
}

// TestConnLimitListener_CaddyModule pins the module ID. A typo here silently
// breaks provisioning at runtime with no compile-time signal.
func TestConnLimitListener_CaddyModule(t *testing.T) {
	info := (&ConnLimitListener{}).CaddyModule()

	if got, want := string(info.ID), "caddy.listeners.vibewarden_conn_limit"; got != want {
		t.Errorf("module ID = %q, want %q", got, want)
	}
	if _, ok := info.New().(*ConnLimitListener); !ok {
		t.Errorf("New() returned %T, want *ConnLimitListener", info.New())
	}
}

// TestConnLimitListener_Provision verifies that a non-positive limit is
// rejected. The config builder never emits the wrapper in that case, so a zero
// or negative value can only come from hand-edited Caddy JSON.
func TestConnLimitListener_Provision(t *testing.T) {
	tests := []struct {
		name    string
		max     int
		wantErr bool
	}{
		{"positive limit is provisioned", 1, false},
		{"large limit is provisioned", 100000, false},
		{"zero limit is rejected", 0, true},
		{"negative limit is rejected", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &ConnLimitListener{MaxConnections: tt.max}
			err := l.Provision(gocaddy.Context{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("Provision() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if !strings.Contains(err.Error(), "max_connections") {
					t.Errorf("Provision() error = %q, want it to name max_connections", err)
				}
				return
			}
			if l.logger == nil {
				t.Error("Provision() left logger nil; want a fallback logger")
			}
		})
	}
}

// TestConnLimitListener_AcceptErrorPropagates verifies that an error from the
// underlying listener is returned unchanged. The wrapper must never swallow it
// into its retry loop, or a closed listener would spin forever.
func TestConnLimitListener_AcceptErrorPropagates(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	limiter := &ConnLimitListener{MaxConnections: 1}
	if err := limiter.Provision(gocaddy.Context{}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	wrapped := limiter.WrapListener(ln)

	if err := wrapped.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, acceptErr := wrapped.Accept()
		done <- acceptErr
	}()

	select {
	case acceptErr := <-done:
		if acceptErr == nil {
			t.Fatal("Accept() on a closed listener returned nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept() on a closed listener did not return")
	}
}
