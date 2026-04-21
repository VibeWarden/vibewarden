package ops_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

type fakeTLSResolver struct {
	state tlsdomain.State
	err   error
}

func (f fakeTLSResolver) Resolve(_ context.Context) (tlsdomain.State, error) {
	return f.state, f.err
}

func TestChainResolver(t *testing.T) {
	tests := []struct {
		name     string
		chain    []ports.TLSStateResolver
		wantKind tlsdomain.Kind
	}{
		{
			name:     "empty chain → unknown",
			chain:    nil,
			wantKind: tlsdomain.KindUnknown,
		},
		{
			name: "primary returns state",
			chain: []ports.TLSStateResolver{
				fakeTLSResolver{state: tlsdomain.NewSelfSignedLocal()},
				fakeTLSResolver{state: tlsdomain.NewObtaining()},
			},
			wantKind: tlsdomain.KindSelfSignedLocal,
		},
		{
			name: "ErrNotInProcess falls through",
			chain: []ports.TLSStateResolver{
				fakeTLSResolver{state: tlsdomain.NewUnknown(), err: ports.ErrNotInProcess},
				fakeTLSResolver{state: tlsdomain.NewSelfSignedLocal()},
			},
			wantKind: tlsdomain.KindSelfSignedLocal,
		},
		{
			name: "all not in process → unknown",
			chain: []ports.TLSStateResolver{
				fakeTLSResolver{state: tlsdomain.NewUnknown(), err: ports.ErrNotInProcess},
				fakeTLSResolver{state: tlsdomain.NewUnknown(), err: ports.ErrNotInProcess},
			},
			wantKind: tlsdomain.KindUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := ops.NewChainResolver(tt.chain...)
			state, err := chain.Resolve(context.Background())
			if err != nil {
				t.Fatalf("Resolve() err = %v", err)
			}
			if state.Kind() != tt.wantKind {
				t.Errorf("Resolve() kind = %v, want %v", state.Kind(), tt.wantKind)
			}
		})
	}
}

func TestChainResolver_NonSentinelError(t *testing.T) {
	boom := errors.New("boom")
	chain := ops.NewChainResolver(
		fakeTLSResolver{state: tlsdomain.NewUnknown(), err: boom},
		fakeTLSResolver{state: tlsdomain.NewSelfSignedLocal()},
	)
	_, err := chain.Resolve(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("expected boom error to propagate, got %v", err)
	}
}
