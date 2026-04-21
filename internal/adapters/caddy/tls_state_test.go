package caddy

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakePeerCertProvider is a minimal peerCertProvider test double.
type fakePeerCertProvider struct {
	leaf *x509.Certificate
	err  error
}

func (f fakePeerCertProvider) LeafCert(_ context.Context) (*x509.Certificate, error) {
	return f.leaf, f.err
}

// makeLeaf returns an x509 leaf with the given issuer CN and NotAfter.
func makeLeaf(issuerCN string, notAfter time.Time) *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.com"},
		Issuer:       pkix.Name{CommonName: issuerCN},
		NotBefore:    notAfter.Add(-30 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
}

func TestInProcessResolver_Resolve(t *testing.T) {
	fixedNow := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	expiryFar := fixedNow.Add(90 * 24 * time.Hour)
	expirySoon := fixedNow.Add(3 * 24 * time.Hour)

	tests := []struct {
		name     string
		cfg      *config.Config
		provider peerCertProvider
		wantKind tlsdomain.Kind
		wantErr  error
	}{
		{
			name:     "nil config disabled",
			cfg:      nil,
			provider: fakePeerCertProvider{},
			wantKind: tlsdomain.KindDisabled,
		},
		{
			name:     "tls disabled",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: false}},
			provider: fakePeerCertProvider{},
			wantKind: tlsdomain.KindDisabled,
		},
		{
			name:     "not in process error",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"}},
			provider: fakePeerCertProvider{err: ports.ErrNotInProcess},
			wantKind: tlsdomain.KindUnknown,
			wantErr:  ports.ErrNotInProcess,
		},
		{
			name:     "self-signed local issuer CN match",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"}},
			provider: fakePeerCertProvider{leaf: makeLeaf(caddyLocalIssuerCN, fixedNow.Add(8*time.Hour))},
			wantKind: tlsdomain.KindSelfSignedLocal,
		},
		{
			name:     "self-signed but no leaf yet → obtaining",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"}},
			provider: fakePeerCertProvider{leaf: nil},
			wantKind: tlsdomain.KindObtaining,
		},
		{
			name:     "acme no leaf → obtaining",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "letsencrypt"}},
			provider: fakePeerCertProvider{leaf: nil},
			wantKind: tlsdomain.KindObtaining,
		},
		{
			name:     "acme healthy leaf → obtained",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "letsencrypt"}},
			provider: fakePeerCertProvider{leaf: makeLeaf("R3", expiryFar)},
			wantKind: tlsdomain.KindObtained,
		},
		{
			name:     "acme leaf near expiry → expiring soon",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "letsencrypt"}},
			provider: fakePeerCertProvider{leaf: makeLeaf("R3", expirySoon)},
			wantKind: tlsdomain.KindExpiringSoon,
		},
		{
			name:     "self-signed with override issuer falls to expiry math",
			cfg:      &config.Config{TLS: config.TLSConfig{Enabled: true, Provider: "self-signed"}},
			provider: fakePeerCertProvider{leaf: makeLeaf("Not Caddy", expiryFar)},
			wantKind: tlsdomain.KindObtained,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewInProcessResolver(tt.cfg).
				withProvider(tt.provider).
				withNow(func() time.Time { return fixedNow })

			state, err := r.Resolve(context.Background())
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Resolve() err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Resolve() unexpected error: %v", err)
			}

			if state.Kind() != tt.wantKind {
				t.Errorf("Resolve() kind = %v, want %v", state.Kind(), tt.wantKind)
			}
		})
	}
}
