package ports

import (
	"context"
	"errors"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// ErrNotInProcess is returned by TLSStateResolver implementations that can
// only operate when Caddy is embedded in the current process (e.g. the
// in-process certmagic resolver). Chain resolvers use this sentinel to fall
// through to network-based resolvers such as the handshake fallback.
var ErrNotInProcess = errors.New("tls state resolver: caddy is not running in this process")

// TLSStateResolver resolves the current TLS state of the running sidecar.
//
// Implementations MUST NOT block on network I/O for longer than the context
// allows. When the resolver has no signal to report, it should return
// tlsdomain.NewUnknown() and a nil error. Returning ErrNotInProcess signals
// that a chain resolver should try the next resolver in the chain.
type TLSStateResolver interface {
	Resolve(ctx context.Context) (tlsdomain.State, error)
}
