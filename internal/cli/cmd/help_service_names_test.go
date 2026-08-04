package cmd_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

// composeTemplatePath is the docker-compose.yml template that is the single
// source of truth for the generated stack's service names.
const composeTemplatePath = "../../config/templates/docker-compose.yml.tmpl"

// composeServiceLine matches a top-level service key inside the services: block
// of the compose template (exactly two leading spaces, no value on the line).
var composeServiceLine = regexp.MustCompile(`^ {2}([a-z0-9][a-z0-9-]*):\s*$`)

// composeTemplateServices parses the compose template and returns the set of
// service names declared under the top-level services: key. Template actions
// ({{ if ... }}) are indented differently and never match the service regex.
func composeTemplateServices(t *testing.T) map[string]bool {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(composeTemplatePath))
	if err != nil {
		t.Fatalf("reading compose template %s: %v", composeTemplatePath, err)
	}

	services := make(map[string]bool)
	inServices := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "services:") {
			inServices = true
			continue
		}
		// A new top-level key (column 0, not a template action) ends the block.
		if inServices && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "{{") {
			break
		}
		if !inServices {
			continue
		}
		if m := composeServiceLine.FindStringSubmatch(line); m != nil {
			services[m[1]] = true
		}
	}

	if len(services) == 0 {
		t.Fatalf("parsed zero services from %s — parser is broken", composeTemplatePath)
	}
	return services
}

// helpText renders the cobra help output for the named subcommand.
func helpText(t *testing.T, name string) string {
	t.Helper()

	root := cmd.NewRootCmd("test")
	sub, _, err := root.Find([]string{name})
	if err != nil || sub == nil {
		t.Fatalf("Find(%s): %v", name, err)
	}

	var out strings.Builder
	sub.SetOut(&out)
	sub.SetErr(&out)
	sub.HelpFunc()(sub, []string{})
	return out.String()
}

// servicesListedInLogsHelp extracts the service names from the indented list
// that follows the "Services in the generated stack" header in the logs help.
func servicesListedInLogsHelp(t *testing.T, help string) []string {
	t.Helper()

	lines := strings.Split(help, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "Services in the generated stack") {
			start = i
			break
		}
	}
	if start == -1 {
		t.Fatalf("logs help text is missing the service-list header:\n%s", help)
	}

	var names []string
	for _, line := range lines[start+1:] {
		if !strings.HasPrefix(line, "  ") {
			// The list ends at the first non-indented (or blank) line. Skip the
			// continuation of the header sentence, which is not indented.
			if len(names) == 0 {
				continue
			}
			break
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		names = append(names, fields[0])
	}

	if len(names) == 0 {
		t.Fatalf("no services parsed from logs help text:\n%s", help)
	}
	return names
}

// TestLogsHelp_ServiceNamesExistInComposeTemplate is an artifact test: every
// service name advertised by "vibew logs --help" must actually be declared in
// the generated docker-compose.yml. Stale names (issue #1338 reported
// "postgres" and "mailslurper") send agents down a dead end because
// "vibew logs <stale-name>" can never match a container.
func TestLogsHelp_ServiceNamesExistInComposeTemplate(t *testing.T) {
	services := composeTemplateServices(t)
	help := helpText(t, "logs")

	for _, name := range servicesListedInLogsHelp(t, help) {
		t.Run(name, func(t *testing.T) {
			if !services[name] {
				t.Errorf("logs help advertises service %q, which is not declared in %s", name, composeTemplatePath)
			}
		})
	}
}

// TestLogsHelp_ObservabilityServiceNames verifies that the observability
// services named in the logs help text match the compose template.
func TestLogsHelp_ObservabilityServiceNames(t *testing.T) {
	services := composeTemplateServices(t)
	help := helpText(t, "logs")

	for _, name := range []string{"prometheus", "loki", "promtail", "otel-collector", "jaeger", "grafana"} {
		t.Run(name, func(t *testing.T) {
			if !services[name] {
				t.Errorf("observability service %q is not declared in %s", name, composeTemplatePath)
			}
			if !strings.Contains(help, name) {
				t.Errorf("logs help text no longer mentions observability service %q", name)
			}
		})
	}
}

// TestHelpText_NoStaleServiceNames guards against the exact regression reported
// in issue #1338: help text naming services the compose template never emits.
func TestHelpText_NoStaleServiceNames(t *testing.T) {
	services := composeTemplateServices(t)

	stale := []string{"postgres", "mailslurper"}
	for _, name := range stale {
		if services[name] {
			t.Fatalf("compose template now declares %q — update this test's stale list", name)
		}
	}

	for _, command := range []string{"logs", "dev"} {
		help := strings.ToLower(helpText(t, command))
		for _, name := range stale {
			t.Run(command+"/"+name, func(t *testing.T) {
				// Word boundaries so that prose like "PostgreSQL database" does
				// not trip the check — only the bare service name is stale.
				re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
				if re.MatchString(help) {
					t.Errorf("%s help text names non-existent service %q; help:\n%s", command, name, help)
				}
			})
		}
	}
}

// TestDevHelp_ServiceNamesExistInComposeTemplate verifies that the compose
// service names given in parentheses by "vibew dev --help" are real.
func TestDevHelp_ServiceNamesExistInComposeTemplate(t *testing.T) {
	services := composeTemplateServices(t)
	help := helpText(t, "dev")

	for _, name := range []string{"vibewarden", "app", "kratos", "kratos-db"} {
		t.Run(name, func(t *testing.T) {
			if !services[name] {
				t.Errorf("dev help advertises service %q, which is not declared in %s", name, composeTemplatePath)
			}
			if !strings.Contains(help, "("+name+")") {
				t.Errorf("dev help text no longer names the %q compose service", name)
			}
		})
	}
}
