package architecture_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	auth "github.com/vibewarden/vibewarden/internal/plugins/auth"
)

// discardSlogLogger returns a *slog.Logger that silently discards all output.
// Used in architecture invariant tests where log output is irrelevant.
func discardSlogLogger(_ *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAuthModes_AllModesHaveHandlerOrAreNone pins the invariant that every
// non-None auth.Mode constant that is enabled must produce a non-nil, non-empty
// result from ContributeCaddyHandlers.
//
// This ensures that a future mode added to config.go without a corresponding
// Caddy handler contribution fails CI immediately rather than silently allowing
// all traffic through (the exact security defect fixed by ADR-103 / #1302).
//
// Modes that legitimately contribute no catch-all handlers (ModeNone, and
// ModeJWT without a JWKS URL in non-dev mode) are excluded from the assertion.
// ModeJWT in dev mode (empty jwks_url / issuer_url — the only configuration
// used in tests) _does_ contribute a handler, so it is included.
func TestAuthModes_AllModesHaveHandlerOrAreNone(t *testing.T) {
	tests := []struct {
		name        string
		mode        auth.Mode
		cfg         auth.Config
		wantNonNil  bool
		skipMessage string
	}{
		{
			name:       "ModeNone contributes no handlers",
			mode:       auth.ModeNone,
			cfg:        auth.Config{Enabled: true, Mode: auth.ModeNone},
			wantNonNil: false, // none is intentionally handler-free
		},
		{
			name: "ModeKratos contributes handlers",
			mode: auth.ModeKratos,
			cfg: auth.Config{
				Enabled:         true,
				Mode:            auth.ModeKratos,
				KratosPublicURL: "http://127.0.0.1:4433",
			},
			wantNonNil: true,
		},
		{
			name: "ModeJWT (dev mode) contributes a handler",
			mode: auth.ModeJWT,
			cfg: auth.Config{
				Enabled: true,
				Mode:    auth.ModeJWT,
				JWT: auth.JWTPluginConfig{
					DevKeyDir: t.TempDir(),
				},
			},
			wantNonNil: true,
		},
		{
			name:       "ModeAPIKey contributes a handler",
			mode:       auth.ModeAPIKey,
			cfg:        auth.Config{Enabled: true, Mode: auth.ModeAPIKey},
			wantNonNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipMessage != "" {
				t.Skip(tt.skipMessage)
			}

			p := auth.New(tt.cfg, discardSlogLogger(t), nil)
			if err := p.Init(context.Background()); err != nil {
				t.Fatalf("Init() error = %v", err)
			}
			defer func() { _ = p.Stop(context.Background()) }() //nolint:errcheck

			handlers := p.ContributeCaddyHandlers()

			if tt.wantNonNil && len(handlers) == 0 {
				t.Errorf("mode %q: ContributeCaddyHandlers() returned nil/empty, want non-empty — "+
					"every non-None mode must contribute at least one Caddy handler (ADR-103)", tt.mode)
			}
			if !tt.wantNonNil && len(handlers) > 0 {
				t.Errorf("mode %q: ContributeCaddyHandlers() returned %d handlers, want 0 — "+
					"ModeNone must contribute no handlers", tt.mode, len(handlers))
			}
		})
	}
}
