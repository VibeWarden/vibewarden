package scaffold

import (
	"context"
	"fmt"
	"path/filepath"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// AddFeatureService orchestrates the `vibew add <feature>` flow:
//  1. Check vibewarden.yaml exists.
//  2. Check feature not already enabled (returns ErrFeatureAlreadyEnabled).
//  3. Enable the feature in vibewarden.yaml.
//  4. Regenerate agent context files when they are present.
type AddFeatureService struct {
	toggler  ports.FeatureToggler
	renderer ports.TemplateRenderer
}

// NewAddFeatureService creates a new AddFeatureService.
func NewAddFeatureService(toggler ports.FeatureToggler, renderer ports.TemplateRenderer) *AddFeatureService {
	return &AddFeatureService{toggler: toggler, renderer: renderer}
}

// AddFeatureOptions carries options for a single `vibew add` invocation.
type AddFeatureOptions struct {
	// Feature is the feature to enable.
	Feature domainscaffold.Feature

	// FeatureOptions contains feature-specific parameters (e.g. TLS domain).
	FeatureOptions domainscaffold.FeatureOptions

	// AgentType controls which agent context files are regenerated.
	// An empty value skips regeneration.
	AgentType domainscaffold.AgentType
}

// AddResult is the result of a successful AddFeature call.
type AddResult struct {
	// UpdatedConfig is the absolute path of the modified vibewarden.yaml.
	UpdatedConfig string

	// RegeneratedContextFiles lists agent context files that were regenerated.
	RegeneratedContextFiles []string
}

// AddFeature enables a feature in vibewarden.yaml inside dir and optionally
// regenerates agent context files.
func (s *AddFeatureService) AddFeature(ctx context.Context, dir string, opts AddFeatureOptions) (*AddResult, error) {
	configPath := filepath.Join(dir, vibeWardenYAML)

	if err := s.toggler.EnableFeature(ctx, configPath, opts.Feature, opts.FeatureOptions); err != nil {
		return nil, fmt.Errorf("enabling feature %q: %w", opts.Feature, err)
	}

	result := &AddResult{UpdatedConfig: configPath}

	// Regenerate agent context files when requested.
	if opts.AgentType != "" {
		state, err := s.toggler.ReadFeatures(ctx, configPath)
		if err != nil {
			return nil, fmt.Errorf("reading updated config: %w", err)
		}

		agentSvc := NewAgentContextService(s.renderer)
		initOpts := InitOptions{
			UpstreamPort:     state.UpstreamPort,
			AuthEnabled:      state.AuthEnabled,
			RateLimitEnabled: state.RateLimitEnabled,
			TLSEnabled:       state.TLSEnabled,
			Force:            true, // always overwrite agent context on regeneration
		}

		written, err := agentSvc.GenerateAgentContext(ctx, dir, opts.AgentType, initOpts)
		if err != nil {
			return nil, fmt.Errorf("regenerating agent context: %w", err)
		}
		result.RegeneratedContextFiles = written
	}

	return result, nil
}
