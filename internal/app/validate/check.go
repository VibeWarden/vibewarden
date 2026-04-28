// Package validate provides runtime checks for vibew validate that go beyond
// strict YAML schema validation. Each check is a local, offline probe that
// detects conditions likely to cause the next vibew command to fail.
//
// All checks follow the same contract: Check(ctx, projectRoot, cfg) returns a
// Result. When Skip is true the caller emits no output row. When Skip is false
// the caller emits one line prefixed with State.String() (two spaces after the
// glyph, matching the ADR-095 OK/OFF/FAIL convention).
package validate

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// Result is the outcome of a single runtime check.
type Result struct {
	// State is StatusOK or StatusFAIL. When Skip is true, State is ignored.
	State ops.StatusState

	// Message is the single-line description emitted after the state glyph.
	// It must not include a trailing newline.
	Message string

	// Source, when non-empty, names the config file that caused this result
	// (e.g. "vibewarden.production.yaml"). An empty Source means the base
	// config file or no specific file attribution. The CLI renderer prefixes
	// FAIL rows with "(Source)" when this field is set.
	Source string

	// Skip, when true, means the check does not apply (e.g. Dockerfile absent).
	// The caller emits no row.
	Skip bool
}

// CheckInputs carries all inputs a check function may need. Using a struct
// instead of a growing argument list lets individual checks access the base
// config separately from the merged (production) config without breaking the
// CheckFunc signature whenever a new input is added.
type CheckInputs struct {
	// ProjectRoot is the absolute path to the directory containing the base
	// vibewarden.yaml. It is used by file-system checks (Dockerfile, .env).
	ProjectRoot string

	// Cfg is the merged (base + production override) config that runtime checks
	// should evaluate. When no production override exists it equals BaseCfg.
	Cfg *config.Config

	// BaseCfg is the config loaded from the base vibewarden.yaml only. Checks
	// that need to distinguish whether a failing value originates from the base
	// or from the production override compare Cfg against BaseCfg.
	BaseCfg *config.Config

	// ProdOverrideExists is true when a vibewarden.production.yaml was
	// discovered and successfully merged into Cfg.
	ProdOverrideExists bool
}

// CheckFunc is the signature every individual check must satisfy.
type CheckFunc func(ctx context.Context, inputs CheckInputs) Result

// RunChecks executes all runtime checks and returns the number of FAIL results.
// Each non-skipped result is appended to results in the order the checks run.
// Callers use the returned slice to render rows and use failures > 0 to set
// exit code 1.
func RunChecks(ctx context.Context, inputs CheckInputs) ([]Result, int) {
	checks := []CheckFunc{
		CheckName,
		CheckDockerfile,
		CheckImageTag,
		CheckACME,
		CheckWAF,
	}

	var results []Result
	failures := 0
	for _, fn := range checks {
		r := fn(ctx, inputs)
		if r.Skip {
			continue
		}
		results = append(results, r)
		if r.State == ops.StatusFAIL {
			failures++
		}
	}
	return results, failures
}
