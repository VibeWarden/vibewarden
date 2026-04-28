// Package promptkickoff renders the canonical agent kickoff prompt that
// vibew owns. It is the single source of truth for what the prompt says;
// both `vibew prompt-template` and the llms-full.txt section originate
// from this package.
//
// The rendered prompt is designed to be pasted directly into a chat session
// with an AI coding agent. It is stdout-only, contains no log lines, and
// the first line is always the version-stamped header so it is immediately
// recognisable.
package promptkickoff

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Sentinel errors returned by Render. Callers should use errors.Is to test
// them, not string comparison.
var (
	// ErrNameRequired is returned when Options.Name is empty.
	ErrNameRequired = errors.New("--name is required")
	// ErrDescribeRequired is returned when Options.Describe is empty (or
	// empty after trimming whitespace).
	ErrDescribeRequired = errors.New("--describe is required")
	// ErrDescribeMultiline is returned when Options.Describe contains a
	// newline or carriage-return character.
	ErrDescribeMultiline = errors.New("--describe must be a single line (no newlines)")
	// ErrDomainRequired is returned when Options.Deploy is true but
	// Options.Domain is empty.
	ErrDomainRequired = errors.New("--domain is required when --deploy is set")
	// ErrVersionRequired is returned when Options.VibewVersion is empty.
	// In practice this should never fire in production builds; the guard
	// exists so unit tests can catch a missing wire-up.
	ErrVersionRequired = errors.New("binary version is unset (internal error)")
)

// Options is the parameter set for rendering the agent kickoff prompt.
type Options struct {
	// Name is the project name. Required. It is sanitised (lowercase,
	// non-alphanumeric characters replaced with hyphens) before rendering.
	Name string

	// Describe is a one-line description of the project. Required. Trimmed
	// of leading/trailing whitespace. Must not contain newline characters.
	Describe string

	// Domain is the FQDN the app will be served on. Required when Deploy
	// is true; ignored otherwise.
	Domain string

	// Deploy selects the flavor: false = dev-only, true = dev + deploy.
	Deploy bool

	// VibewVersion is the version string embedded in the prompt header.
	// Populated by the CLI command from the binary's version field.
	VibewVersion string
}

// templateData is the data payload passed to text/template execution.
// Fields are exported so text/template can access them.
type templateData struct {
	Name         string
	Describe     string
	Domain       string
	VibewVersion string
}

// Service renders the kickoff prompt against an embedded text/template.
type Service struct {
	renderer ports.TemplateRenderer
}

// NewService creates a Service backed by the supplied renderer.
// The renderer must be able to access the prompt templates by name
// (e.g. "prompts/dev.tmpl", "prompts/deploy.tmpl") from the FS it wraps.
func NewService(renderer ports.TemplateRenderer) *Service {
	return &Service{renderer: renderer}
}

// Render validates opts, picks the appropriate template, renders it, and
// returns the prompt as bytes ready for writing to stdout.
//
// All validation errors are typed sentinels (ErrNameRequired, etc.) so callers
// can distinguish them with errors.Is. No partial output is ever returned
// together with a non-nil error.
func (s *Service) Render(opts Options) ([]byte, error) {
	if err := validate(opts); err != nil {
		return nil, err
	}

	tmplName := "prompts/dev.tmpl"
	if opts.Deploy {
		tmplName = "prompts/deploy.tmpl"
	}

	data := templateData{
		Name:         config.SanitizeProjectName(opts.Name),
		Describe:     strings.TrimSpace(opts.Describe),
		Domain:       opts.Domain,
		VibewVersion: opts.VibewVersion,
	}

	// The deploy template always needs a domain. The dev template includes
	// it too so that `vibew add tls --domain <domain>` is populated. When
	// Deploy is false and no domain was supplied, fall back to a clear
	// placeholder so the dev flavor still renders correctly.
	if data.Domain == "" {
		data.Domain = "<your-domain>"
	}

	out, err := s.renderer.Render(tmplName, data)
	if err != nil {
		return nil, fmt.Errorf("rendering prompt template %q: %w", tmplName, err)
	}
	return out, nil
}

// validate checks all fields in opts and returns the first validation error
// it encounters.
func validate(opts Options) error {
	if opts.VibewVersion == "" {
		return ErrVersionRequired
	}
	if opts.Name == "" {
		return ErrNameRequired
	}
	describe := strings.TrimSpace(opts.Describe)
	if describe == "" {
		return ErrDescribeRequired
	}
	if strings.ContainsAny(opts.Describe, "\n\r") {
		return ErrDescribeMultiline
	}
	if opts.Deploy && opts.Domain == "" {
		return ErrDomainRequired
	}
	return nil
}
