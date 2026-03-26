package scaffold

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/cli/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// agentSpec maps an AgentType to its template name and output path.
type agentSpec struct {
	templateName string
	outputPath   string
}

// agentSpecs defines the template and output path for each supported agent type.
var agentSpecs = map[scaffold.AgentType]agentSpec{
	scaffold.AgentTypeClaude:  {templateName: "claude.md.tmpl", outputPath: filepath.Join(".claude", "CLAUDE.md")},
	scaffold.AgentTypeCursor:  {templateName: "cursor-rules.tmpl", outputPath: filepath.Join(".cursor", "rules")},
	scaffold.AgentTypeGeneric: {templateName: "agents.md.tmpl", outputPath: "AGENTS.md"},
}

// allAgentTypes is the ordered slice used when AgentTypeAll is requested.
var allAgentTypes = []scaffold.AgentType{
	scaffold.AgentTypeClaude,
	scaffold.AgentTypeCursor,
	scaffold.AgentTypeGeneric,
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
	agentType scaffold.AgentType,
	opts InitOptions,
) ([]string, error) {
	data := scaffold.AgentContextData{
		UpstreamPort:     opts.UpstreamPort,
		AuthEnabled:      opts.AuthEnabled,
		RateLimitEnabled: opts.RateLimitEnabled,
		TLSEnabled:       opts.TLSEnabled,
		RateLimitRPS:     10, // sensible default matching vibewarden.yaml template
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
func resolveAgentTypes(agentType scaffold.AgentType) []scaffold.AgentType {
	if agentType == scaffold.AgentTypeAll {
		return allAgentTypes
	}
	return []scaffold.AgentType{agentType}
}
