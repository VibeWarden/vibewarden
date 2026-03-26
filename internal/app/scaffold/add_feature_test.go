package scaffold_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// fakeToggler is a test double for ports.FeatureToggler.
type fakeToggler struct {
	state          *domainscaffold.FeatureState
	enableErr      error
	readErr        error
	enabledFeature domainscaffold.Feature
	enabledOpts    domainscaffold.FeatureOptions
}

func (f *fakeToggler) ReadFeatures(_ context.Context, _ string) (*domainscaffold.FeatureState, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	if f.state == nil {
		return &domainscaffold.FeatureState{}, nil
	}
	return f.state, nil
}

func (f *fakeToggler) EnableFeature(_ context.Context, _ string, feature domainscaffold.Feature, opts domainscaffold.FeatureOptions) error {
	f.enabledFeature = feature
	f.enabledOpts = opts
	return f.enableErr
}

func TestAddFeatureService_AddFeature(t *testing.T) {
	baseState := &domainscaffold.FeatureState{
		UpstreamPort:     3000,
		RateLimitEnabled: true,
	}

	tests := []struct {
		name               string
		opts               scaffoldapp.AddFeatureOptions
		togglerEnableErr   error
		togglerReadErr     error
		togglerState       *domainscaffold.FeatureState
		wantErr            bool
		wantErrIs          error
		wantFeatureEnabled domainscaffold.Feature
		wantAgentFiles     bool
	}{
		{
			name: "adds auth feature without agent context",
			opts: scaffoldapp.AddFeatureOptions{
				Feature: domainscaffold.FeatureAuth,
			},
			togglerState:       baseState,
			wantFeatureEnabled: domainscaffold.FeatureAuth,
		},
		{
			name: "adds tls feature with options",
			opts: scaffoldapp.AddFeatureOptions{
				Feature: domainscaffold.FeatureTLS,
				FeatureOptions: domainscaffold.FeatureOptions{
					TLSDomain:   "example.com",
					TLSProvider: "letsencrypt",
				},
			},
			togglerState:       baseState,
			wantFeatureEnabled: domainscaffold.FeatureTLS,
		},
		{
			name: "already enabled feature propagates ErrFeatureAlreadyEnabled",
			opts: scaffoldapp.AddFeatureOptions{
				Feature: domainscaffold.FeatureAuth,
			},
			togglerEnableErr: fmt.Errorf("add auth: %w", domainscaffold.ErrFeatureAlreadyEnabled),
			wantErr:          true,
			wantErrIs:        domainscaffold.ErrFeatureAlreadyEnabled,
		},
		{
			name: "toggler enable error is propagated",
			opts: scaffoldapp.AddFeatureOptions{
				Feature: domainscaffold.FeatureRateLimit,
			},
			togglerEnableErr: errors.New("disk failure"),
			wantErr:          true,
		},
		{
			name: "agent type set triggers context regeneration",
			opts: scaffoldapp.AddFeatureOptions{
				Feature:   domainscaffold.FeatureAdmin,
				AgentType: domainscaffold.AgentTypeClaude,
			},
			togglerState:   baseState,
			wantAgentFiles: true,
		},
		{
			name: "read features error after enable is propagated",
			opts: scaffoldapp.AddFeatureOptions{
				Feature:   domainscaffold.FeatureMetrics,
				AgentType: domainscaffold.AgentTypeClaude,
			},
			togglerReadErr: errors.New("read error"),
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			// Write a stub vibewarden.yaml so the service has a valid path.
			cfgPath := filepath.Join(dir, "vibewarden.yaml")
			if err := os.WriteFile(cfgPath, []byte("server:\n  port: 8080\n"), 0o644); err != nil {
				t.Fatalf("writing stub config: %v", err)
			}

			toggler := &fakeToggler{
				state:     tt.togglerState,
				enableErr: tt.togglerEnableErr,
				readErr:   tt.togglerReadErr,
			}
			renderer := newFakeRenderer()
			svc := scaffoldapp.NewAddFeatureService(toggler, renderer)

			result, err := svc.AddFeature(context.Background(), dir, tt.opts)

			if (err != nil) != tt.wantErr {
				t.Fatalf("AddFeature() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
				t.Errorf("errors.Is(%v) = false, got %v", tt.wantErrIs, err)
			}
			if tt.wantErr {
				return
			}

			if toggler.enabledFeature != tt.wantFeatureEnabled && tt.wantFeatureEnabled != "" {
				t.Errorf("enabled feature = %q, want %q", toggler.enabledFeature, tt.wantFeatureEnabled)
			}

			if result.UpdatedConfig == "" {
				t.Error("UpdatedConfig must not be empty")
			}

			if tt.wantAgentFiles && len(result.RegeneratedContextFiles) == 0 {
				t.Error("expected agent context files to be regenerated")
			}
		})
	}
}

// Compile-time check: fakeToggler satisfies the FeatureToggler interface shape.
var _ interface {
	ReadFeatures(context.Context, string) (*domainscaffold.FeatureState, error)
	EnableFeature(context.Context, string, domainscaffold.Feature, domainscaffold.FeatureOptions) error
} = (*fakeToggler)(nil)
