package ops_test

import (
	"os"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/ops"
)

// TestMain overrides the sidecar settle duration for all tests in this
// package so that tests exercising DevService.Run do not incur a real
// 5-second wait.
func TestMain(m *testing.M) {
	ops.SidecarSettleDuration = time.Millisecond
	os.Exit(m.Run())
}
