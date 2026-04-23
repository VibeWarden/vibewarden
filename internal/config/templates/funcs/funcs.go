// Package funcs provides the shared text/template function map used by all
// VibeWarden template renderers.
//
// These are pure, stateless helpers with no external dependencies beyond stdlib.
// Both internal/adapters/template and internal/app/bundle import this package
// as the single source of truth for custom template vocabulary, avoiding any
// internal/app → internal/adapters dependency.
package funcs

import (
	"fmt"
	"text/template"
)

// FuncMap returns the custom template.FuncMap available to all VibeWarden
// templates. The returned map is a new allocation on every call; callers may
// mutate it freely without affecting other callers.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"mul":            mul,
		"healthcheckCmd": healthcheckCmd,
	}
}

// mul multiplies two integers and returns the product.
// Used in loki-config.yml.tmpl to convert retention days to hours.
func mul(a, b int) int { return a * b }

// healthcheckCmd returns a Docker health check command appropriate for the
// given language runtime. Images based on Alpine (Go, Kotlin) ship wget;
// Python and Node slim images do not but always have their own runtime
// available. Falls back to wget for unknown languages.
//
// IMPORTANT: the returned string is placed inside a YAML double-quoted string
// (["CMD-SHELL", "..."]), so inner double quotes would break the YAML parse.
// Python and Node commands use single quotes for their code argument to avoid
// this.
func healthcheckCmd(lang string, port int) string {
	switch lang {
	case "python":
		return fmt.Sprintf(
			`python -c 'import urllib.request; urllib.request.urlopen("http://127.0.0.1:%d/health")'`,
			port)
	case "typescript", "javascript":
		return fmt.Sprintf(
			`node -e 'const h=require("http");h.get("http://127.0.0.1:%d/health",r=>{process.exit(r.statusCode===200?0:1)}).on("error",()=>process.exit(1))'`,
			port)
	default: // go, kotlin, alpine-based, unknown
		return fmt.Sprintf(
			`wget -q --spider http://127.0.0.1:%d/health || exit 1`,
			port)
	}
}
