package validate

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// CheckWAF detects when the merged (production) config has WAF enabled with
// mode: log. In production, log mode is appropriate for staging rollouts but
// not for permanent use — it silently drops detections without blocking. The
// check fires only when a vibewarden.production.yaml was discovered (indicated
// by prodOverrideExists == true), ensuring it is a production concern.
//
// Skip conditions (no row emitted):
//   - prodOverrideExists is false (no production override detected).
//   - cfg.WAF.Enabled is false.
//   - cfg.WAF.Mode is not "log".
//
// When cfg.WAF.AcknowledgeLogMode is true the check emits OK instead of FAIL,
// signalling that the operator has explicitly accepted log mode in production.
func CheckWAF(_ context.Context, _ string, cfg *config.Config, prodOverrideExists bool) Result {
	if !prodOverrideExists {
		return Result{Skip: true}
	}
	if !cfg.WAF.Enabled {
		return Result{Skip: true}
	}
	if cfg.WAF.Mode != "log" {
		return Result{Skip: true}
	}

	if cfg.WAF.AcknowledgeLogMode {
		return Result{
			State:   ops.StatusOK,
			Message: "WAF log-mode acknowledged",
		}
	}

	return Result{
		State: ops.StatusFAIL,
		Message: "WAF is enabled in production with mode: log — this is suitable for staging but not production; " +
			"set waf.mode: block or add waf.acknowledge_log_mode: true to suppress this check",
	}
}
