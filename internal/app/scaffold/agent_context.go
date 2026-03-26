package scaffold

import (
	"context"
	"fmt"
	"path/filepath"

	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// agentSpec maps an AgentType to its template name and output path.
type agentSpec struct {
	templateName string
	outputPath   string
}

// agentSpecs defines the template and output path for each supported agent type.
var agentSpecs = map[domainscaffold.AgentType]agentSpec{
	domainscaffold.AgentTypeClaude:  {templateName: "claude.md.tmpl", outputPath: filepath.Join(".claude", "CLAUDE.md")},
	domainscaffold.AgentTypeCursor:  {templateName: "cursor-rules.tmpl", outputPath: filepath.Join(".cursor", "rules")},
	domainscaffold.AgentTypeGeneric: {templateName: "agents.md.tmpl", outputPath: "AGENTS.md"},
}

// allAgentTypes is the ordered slice used when AgentTypeAll is requested.
var allAgentTypes = []domainscaffold.AgentType{
	domainscaffold.AgentTypeClaude,
	domainscaffold.AgentTypeCursor,
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
// Files that already exist are skipped unless opts.Force is true.
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
		RateLimitRPS:     10, // TODO: must stay in sync with the rate_limit default in vibewarden.yaml template
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
		if err := s.renderer.RenderToFile(spec.templateName, data, outPath, opts.Force); err != nil {
			return nil, fmt.Errorf("generating context for agent %q: %w", at, err)
		}
		written = append(written, outPath)
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
