package ops

import (
	"context"
	"errors"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ChainResolver composes a sequence of ports.TLSStateResolver implementations.
// The first resolver's result wins unless it returns ports.ErrNotInProcess,
// in which case the chain falls through to the next resolver.
//
// This is the canonical way to wire the in-process certmagic resolver as a
// preferred primary with a handshake fallback. When all resolvers in the
// chain report ErrNotInProcess (or the chain is empty) the zero-value
// tlsdomain.NewUnknown() is returned.
type ChainResolver struct {
	resolvers []ports.TLSStateResolver
}

// NewChainResolver builds a ChainResolver. Resolvers are tried in order.
func NewChainResolver(resolvers ...ports.TLSStateResolver) *ChainResolver {
	return &ChainResolver{resolvers: resolvers}
}

// Resolve implements ports.TLSStateResolver.
func (c *ChainResolver) Resolve(ctx context.Context) (tlsdomain.State, error) {
	var lastErr error
	for _, r := range c.resolvers {
		state, err := r.Resolve(ctx)
		if errors.Is(err, ports.ErrNotInProcess) {
			lastErr = err
			continue
		}
		if err != nil {
			return state, err
		}
		return state, nil
	}
	// All resolvers fell through. The chain as a whole is Unknown — not an
	// error the caller should surface.
	_ = lastErr
	return tlsdomain.NewUnknown(), nil
}
