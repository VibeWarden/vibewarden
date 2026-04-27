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

	// Skip, when true, means the check does not apply (e.g. Dockerfile absent).
	// The caller emits no row.
	Skip bool
}

// CheckFunc is the signature every individual check must satisfy.
type CheckFunc func(ctx context.Context, projectRoot string, cfg *config.Config, prodOverrideExists bool) Result

// RunChecks executes all runtime checks and returns the number of FAIL results.
// Each non-skipped result is appended to results in the order the checks run.
// Callers use the returned slice to render rows and use failures > 0 to set
// exit code 1.
func RunChecks(ctx context.Context, projectRoot string, cfg *config.Config, prodOverrideExists bool) ([]Result, int) {
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
		r := fn(ctx, projectRoot, cfg, prodOverrideExists)
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
