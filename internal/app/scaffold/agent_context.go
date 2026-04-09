package scaffold

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// defaultRateLimitRPS is the default requests-per-second value written into
// generated agent context files. It must stay in sync with the rate_limit
// per_ip.requests_per_second default in the vibewarden.yaml.tmpl template.
const defaultRateLimitRPS = 10

// AgentContextService generates AI agent context files for a project.
type AgentContextService struct {
	renderer ports.TemplateRenderer
}

// NewAgentContextService creates a new AgentContextService.
func NewAgentContextService(renderer ports.TemplateRenderer) *AgentContextService {
	return &AgentContextService{renderer: renderer}
}

// GenerateAgentContext generates AGENTS-VIBEWARDEN.md and AGENTS.md in dir.
//
// AGENTS-VIBEWARDEN.md is always overwritten (it is vibew-owned).
// AGENTS.md is created from template when absent, or the reference line is
// appended when it is missing from an existing file.
//
// Returns the list of file paths written.
func (s *AgentContextService) GenerateAgentContext(
	_ context.Context,
	dir string,
	opts InitOptions,
) ([]string, error) {
	data := domainscaffold.AgentContextData{
		UpstreamPort:     opts.UpstreamPort,
		AuthEnabled:      opts.AuthEnabled,
		RateLimitEnabled: opts.RateLimitEnabled,
		TLSEnabled:       opts.TLSEnabled,
		RateLimitRPS:     defaultRateLimitRPS,
		AdminEnabled:     false,
	}

	// Render AGENTS-VIBEWARDEN.md from the agents/agents-vibewarden.md.tmpl
	// template. This file is always overwritten — it is vibew-owned.
	vibewardenPath := filepath.Join(dir, "AGENTS-VIBEWARDEN.md")
	if err := s.renderer.RenderToFile("agents/agents-vibewarden.md.tmpl", data, vibewardenPath, true); err != nil {
		return nil, fmt.Errorf("generating AGENTS-VIBEWARDEN.md: %w", err)
	}

	// Ensure AGENTS.md exists and contains a reference to AGENTS-VIBEWARDEN.md.
	agentsMDPath := filepath.Join(dir, "AGENTS.md")
	if err := ensureAgentsMD(s.renderer, agentsMDPath); err != nil {
		return nil, fmt.Errorf("ensuring AGENTS.md: %w", err)
	}

	return []string{vibewardenPath, agentsMDPath}, nil
}

// ensureAgentsMD ensures AGENTS.md at dest exists and contains a reference to
// AGENTS-VIBEWARDEN.md.
//
// Behaviour:
//   - If AGENTS.md does not exist, it is created from agents/agents.md.tmpl.
//   - If AGENTS.md exists but does not contain a reference to AGENTS-VIBEWARDEN.md,
//     the reference line is appended.
//   - If AGENTS.md already contains the reference, it is left unchanged.
//
// The reference detection uses a simple substring match for "AGENTS-VIBEWARDEN.md".
func ensureAgentsMD(renderer ports.TemplateRenderer, dest string) error {
	existing, err := os.ReadFile(dest) //nolint:gosec // path is constructed from trusted inputs
	if errors.Is(err, os.ErrNotExist) {
		// Create from template.
		if createErr := renderer.RenderToFile("agents/agents.md.tmpl", nil, dest, false); createErr != nil {
			return fmt.Errorf("creating AGENTS.md: %w", createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading AGENTS.md: %w", err)
	}

	// File exists — check whether the reference is already present.
	if strings.Contains(string(existing), "AGENTS-VIBEWARDEN.md") {
		return nil
	}

	// Append reference.
	f, err := os.OpenFile(dest, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // path is trusted
	if err != nil {
		return fmt.Errorf("opening AGENTS.md for append: %w", err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on read path

	const referenceBlock = "\n\nSee [AGENTS-VIBEWARDEN.md](./AGENTS-VIBEWARDEN.md) for VibeWarden sidecar instructions.\n"
	if _, writeErr := f.WriteString(referenceBlock); writeErr != nil {
		return fmt.Errorf("appending reference to AGENTS.md: %w", writeErr)
	}
	return nil
}
