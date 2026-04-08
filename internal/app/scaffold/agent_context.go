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

// agentSpec maps an AgentType to its template name and output path.
type agentSpec struct {
	templateName string
	outputPath   string
}

// agentSpecs defines the template and output path for each supported agent type.
// AgentTypeGeneric is handled separately: it generates AGENTS-VIBEWARDEN.md and
// then calls ensureAgentsMD to create or update AGENTS.md with a reference.
var agentSpecs = map[domainscaffold.AgentType]agentSpec{
	domainscaffold.AgentTypeClaude:  {templateName: "claude.md.tmpl", outputPath: filepath.Join(".claude", "CLAUDE.md")},
	domainscaffold.AgentTypeGeneric: {templateName: "agents.md.tmpl", outputPath: "AGENTS-VIBEWARDEN.md"},
}

// allAgentTypes is the ordered slice used when AgentTypeAll is requested.
var allAgentTypes = []domainscaffold.AgentType{
	domainscaffold.AgentTypeClaude,
	domainscaffold.AgentTypeGeneric,
}

// AgentContextService generates AI agent context files for a project.
type AgentContextService struct {
	renderer ports.TemplateRenderer
}

// NewAgentContextService creates a new AgentContextService.
func NewAgentContextService(renderer ports.TemplateRenderer) *AgentContextService {
	return &AgentContextService{renderer: renderer}
}

// GenerateAgentContext generates AI agent context files in dir for the given
// agent type and init options.
//
// When agentType is AgentTypeAll, context files for all supported agent types
// are generated. Returns the list of file paths written.
//
// For AgentTypeGeneric: AGENTS-VIBEWARDEN.md is always overwritten (it is
// vibew-owned). AGENTS.md is then created from template when absent, or the
// reference line is appended when it is missing from an existing file.
//
// Files other than AGENTS-VIBEWARDEN.md that already exist are skipped unless
// opts.Force is true.
func (s *AgentContextService) GenerateAgentContext(
	_ context.Context,
	dir string,
	agentType domainscaffold.AgentType,
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

	types := resolveAgentTypes(agentType)

	var written []string
	for _, at := range types {
		spec, ok := agentSpecs[at]
		if !ok {
			return nil, fmt.Errorf("unknown agent type %q", at)
		}

		outPath := filepath.Join(dir, spec.outputPath)

		if at == domainscaffold.AgentTypeGeneric {
			// AGENTS-VIBEWARDEN.md is always overwritten (vibew-owned).
			if err := s.renderer.RenderToFile(spec.templateName, data, outPath, true); err != nil {
				return nil, fmt.Errorf("generating context for agent %q: %w", at, err)
			}
			written = append(written, outPath)

			// Ensure AGENTS.md exists and contains a reference to AGENTS-VIBEWARDEN.md.
			agentsMDPath := filepath.Join(dir, "AGENTS.md")
			if err := ensureAgentsMD(s.renderer, agentsMDPath); err != nil {
				return nil, fmt.Errorf("ensuring AGENTS.md: %w", err)
			}
			written = append(written, agentsMDPath)
		} else {
			if err := s.renderer.RenderToFile(spec.templateName, data, outPath, opts.Force); err != nil {
				return nil, fmt.Errorf("generating context for agent %q: %w", at, err)
			}
			written = append(written, outPath)
		}
	}

	return written, nil
}

// resolveAgentTypes expands AgentTypeAll into the full list; otherwise returns
// a single-element slice.
func resolveAgentTypes(agentType domainscaffold.AgentType) []domainscaffold.AgentType {
	if agentType == domainscaffold.AgentTypeAll {
		return allAgentTypes
	}
	return []domainscaffold.AgentType{agentType}
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
