package caddy

import (
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	gocaddy "github.com/caddyserver/caddy/v2"
)

func init() {
	gocaddy.RegisterModule(&ConnLimitListener{})
}

// connLimitRefusalLogInterval is the minimum time between refusal warnings.
// A log line per refused connection would turn a connection flood into a
// denial of service against the log sink, so refusals are counted and
// summarised at most once per interval.
const connLimitRefusalLogInterval = 30 * time.Second

// ConnLimitListener is a Caddy listener wrapper module that caps the number of
// concurrent connections held on the listener it wraps.
//
// When the cap is reached, further connections are accepted and immediately
// closed (refused) rather than left waiting in the kernel backlog. Refusing
// eagerly releases the file descriptor at once and gives the client a prompt
// FIN/RST instead of an indefinite hang, which is why this module exists
// instead of golang.org/x/net/netutil.LimitListener (that one blocks in
// Accept until a slot frees).
//
// Established connections and in-flight requests are never affected: the cap
// only governs admission of new connections.
//
// The module is registered under the ID "caddy.listeners.vibewarden_conn_limit"
// and referenced from the Caddy JSON configuration as:
//
//	{"wrapper": "vibewarden_conn_limit", "max_connections": 1000}
//
// It must be placed before the {"wrapper": "tls"} placeholder in the
// listener_wrappers list so the cap applies to the raw TCP listener rather
// than to post-handshake connections. See ADR-110.
//
// HTTP/3 is not covered: QUIC arrives over a net.PacketConn, not a
// net.Listener. That is acceptable because QUIC multiplexes every connection
// over a single UDP socket and so cannot exhaust file descriptors, but it does
// mean this is a connection cap and not a general concurrency limit.
type ConnLimitListener struct {
	// MaxConnections is the maximum number of concurrent connections held on
	// the wrapped listener. Must be greater than zero — the config builder
	// omits the wrapper entirely when the limit is disabled.
	MaxConnections int `json:"max_connections,omitempty"`

	logger  *slog.Logger
	active  atomic.Int64
	refused atomic.Int64
	lastLog atomic.Int64 // unix nanos of the last refusal warning
}

// Interface guards.
var (
	_ gocaddy.ListenerWrapper = (*ConnLimitListener)(nil)
	_ gocaddy.Provisioner     = (*ConnLimitListener)(nil)
)

// CaddyModule returns the module metadata used to register it with Caddy.
// The receiver is a pointer because the struct carries atomic counters, which
// must never be copied.
func (*ConnLimitListener) CaddyModule() gocaddy.ModuleInfo {
	return gocaddy.ModuleInfo{
		ID:  "caddy.listeners.vibewarden_conn_limit",
		New: func() gocaddy.Module { return new(ConnLimitListener) },
	}
}

// Provision implements gocaddy.Provisioner. It resolves the shared logger from
// the runtime services registry and rejects a non-positive limit, which the
// config builder never emits and therefore indicates hand-edited Caddy JSON.
func (l *ConnLimitListener) Provision(_ gocaddy.Context) error {
	if l.MaxConnections <= 0 {
		return fmt.Errorf("max_connections must be > 0, got %d", l.MaxConnections)
	}

	l.logger = currentServices().Logger
	if l.logger == nil {
		l.logger = slog.Default()
	}
	return nil
}

// WrapListener implements gocaddy.ListenerWrapper. It returns a listener whose
// Accept refuses connections beyond MaxConnections.
func (l *ConnLimitListener) WrapListener(ln net.Listener) net.Listener {
	return &connLimitedListener{Listener: ln, limit: l}
}

// noteRefusal records a refused connection and emits a throttled warning.
func (l *ConnLimitListener) noteRefusal() {
	total := l.refused.Add(1)

	now := time.Now().UnixNano()
	last := l.lastLog.Load()
	if now-last < int64(connLimitRefusalLogInterval) {
		return
	}
	if !l.lastLog.CompareAndSwap(last, now) {
		// Another goroutine just logged; skip this one.
		return
	}

	logger := l.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("connection refused: max_connections reached",
		slog.Int("max_connections", l.MaxConnections),
		slog.Int64("refused_total", total),
	)
}

// connLimitedListener enforces the concurrent-connection cap of its owning
// ConnLimitListener. Close and Addr delegate to the embedded net.Listener.
type connLimitedListener struct {
	net.Listener
	limit *ConnLimitListener
}

// Accept returns the next admitted connection. Connections arriving while the
// cap is reached are accepted and immediately closed, and the accept loop
// continues — Accept never manufactures an error for a refusal, because an
// error return would make Caddy tear the whole server down.
func (l *connLimitedListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		if int(l.limit.active.Add(1)) > l.limit.MaxConnections {
			l.limit.active.Add(-1)
			// Record the refusal before closing: closing is what the client
			// observes (FIN/RST), so anything that sees the reset must already
			// see the counter and the log line. Recording afterwards leaves a
			// window where the refusal is visible on the wire but not in the
			// metrics, which is a real observability gap and not only a test
			// race. Throttling keeps this off the hot path under a flood.
			l.limit.noteRefusal()
			_ = conn.Close()
			continue
		}

		return &limitedConn{Conn: conn, limit: l.limit}, nil
	}
}

// limitedConn releases its slot in the connection cap exactly once, when the
// connection is closed.
type limitedConn struct {
	net.Conn
	limit *ConnLimitListener
	once  sync.Once
}

// Close closes the underlying connection and releases its slot in the cap.
// It is safe to call more than once; the slot is released only on the first
// call.
func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.limit.active.Add(-1) })
	return err
}
