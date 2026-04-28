package validate

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// CheckWAF detects when the merged (production) config has WAF enabled with
// mode: log. In production, log mode is appropriate for staging rollouts but
// not for permanent use — it silently drops detections without blocking. The
// check fires only when a vibewarden.production.yaml was discovered (indicated
// by inputs.ProdOverrideExists == true), ensuring it is a production concern.
//
// Skip conditions (no row emitted):
//   - inputs.ProdOverrideExists is false (no production override detected).
//   - inputs.Cfg.WAF.Enabled is false.
//   - inputs.Cfg.WAF.Mode is not "log".
//
// When inputs.Cfg.WAF.AcknowledgeLogMode is true the check emits OK instead of
// FAIL, signalling that the operator has explicitly accepted log mode in
// production.
//
// FAIL rows are always attributed to "vibewarden.production.yaml" because the
// check only fires when a production override exists and its WAF configuration
// is what carries the failing value.
func CheckWAF(_ context.Context, inputs CheckInputs) Result {
	if !inputs.ProdOverrideExists {
		return Result{Skip: true}
	}
	if !inputs.Cfg.WAF.Enabled {
		return Result{Skip: true}
	}
	if inputs.Cfg.WAF.Mode != "log" {
		return Result{Skip: true}
	}

	if inputs.Cfg.WAF.AcknowledgeLogMode {
		return Result{
			State:   ops.StatusOK,
			Message: "WAF log-mode acknowledged",
		}
	}

	return Result{
		State:  ops.StatusFAIL,
		Source: "vibewarden.production.yaml",
		Message: "WAF is enabled in production with mode: log — this is suitable for staging but not production; " +
			"set waf.mode: block or add waf.acknowledge_log_mode: true to suppress this check",
	}
}
