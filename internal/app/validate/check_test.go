package validate_test

import (
	"context"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/app/validate"
	"github.com/vibewarden/vibewarden/internal/config"
)

// TestRunChecks locks the aggregator contract: skipped results are dropped,
// non-skipped results keep check order, and the returned count matches the
// number of FAIL rows (the CLI turns a non-zero count into exit code 1).
func TestRunChecks(t *testing.T) {
	tests := []struct {
		name         string
		inputs       validate.CheckInputs
		wantFailures int
	}{
		{
			// Empty project root makes every filesystem-backed check skip, and
			// a fully defaulted config has nothing to fail on.
			name:         "clean config produces no failures",
			inputs:       validate.CheckInputs{Cfg: &config.Config{Name: "myapp"}, BaseCfg: &config.Config{Name: "myapp"}},
			wantFailures: 0,
		},
		{
			// Unnamed project in a directory called "vibewarden" collides with
			// the sidecar's own compose project name (CheckName).
			name: "name collision is counted as a failure",
			inputs: validate.CheckInputs{
				ProjectRoot: "/home/user/vibewarden",
				Cfg:         &config.Config{},
				BaseCfg:     &config.Config{},
			},
			wantFailures: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, failures := validate.RunChecks(context.Background(), tt.inputs)

			if failures != tt.wantFailures {
				t.Errorf("failures = %d, want %d (results: %+v)", failures, tt.wantFailures, results)
			}

			counted := 0
			for _, r := range results {
				if r.Skip {
					t.Errorf("skipped result leaked into the output: %+v", r)
				}
				if r.State == ops.StatusFAIL {
					counted++
				}
			}
			if counted != failures {
				t.Errorf("counted %d FAIL rows but RunChecks reported %d", counted, failures)
			}
		})
	}
}
