package plugins

import (
	"context"
	"log/slog"
	"testing"

	domainheal "github.com/vibewarden/vibewarden/internal/domain/health"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// stubPlugin is a minimal ports.Plugin stub for readiness tests.
type stubPlugin struct {
	name    string
	healthy bool
	message string
}

func (s *stubPlugin) Name() string                  { return s.name }
func (s *stubPlugin) Init(_ context.Context) error  { return nil }
func (s *stubPlugin) Start(_ context.Context) error { return nil }
func (s *stubPlugin) Stop(_ context.Context) error  { return nil }
func (s *stubPlugin) Health() ports.HealthStatus {
	return ports.HealthStatus{Healthy: s.healthy, Message: s.message}
}

// stubUpstreamChecker implements ports.UpstreamHealthChecker for readiness tests.
type stubUpstreamChecker struct {
	status domainheal.UpstreamStatus
}

func (s *stubUpstreamChecker) Start(_ context.Context) error { return nil }
func (s *stubUpstreamChecker) Stop(_ context.Context) error  { return nil }
func (s *stubUpstreamChecker) CurrentStatus() domainheal.UpstreamStatus {
	return s.status
}
func (s *stubUpstreamChecker) Snapshot() ports.UpstreamHealthSnapshot {
	return ports.UpstreamHealthSnapshot{Status: s.status.String()}
}

var _ ports.UpstreamHealthChecker = (*stubUpstreamChecker)(nil)

func TestRegistry_ReadinessChecker_NoPlugins_NoUpstream_IsReady(t *testing.T) {
	r := NewRegistry(slog.Default())
	rc := r.ReadinessChecker(nil)

	if !rc.Ready() {
		t.Error("Ready() = false, want true (no plugins, no upstream checker)")
	}

	rs := rc.ReadinessStatus()
	if !rs.PluginsReady {
		t.Error("PluginsReady = false, want true (no plugins)")
	}
	if !rs.UpstreamReachable {
		t.Error("UpstreamReachable = false, want true (no upstream checker configured)")
	}
}

func TestRegistry_ReadinessChecker_AllPluginsHealthy_IsReady(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "tls", healthy: true})
	r.Register(&stubPlugin{name: "rate-limiting", healthy: true})

	rc := r.ReadinessChecker(nil)

	if !rc.Ready() {
		t.Error("Ready() = false, want true (all plugins healthy)")
	}

	rs := rc.ReadinessStatus()
	if !rs.PluginsReady {
		t.Error("PluginsReady = false, want true")
	}
	if len(rs.Plugins) != 2 {
		t.Errorf("len(Plugins) = %d, want 2", len(rs.Plugins))
	}
}

func TestRegistry_ReadinessChecker_UnhealthyPlugin_NotReady(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "tls", healthy: true})
	r.Register(&stubPlugin{name: "user-management", healthy: false, message: "postgres unreachable"})

	rc := r.ReadinessChecker(nil)

	if rc.Ready() {
		t.Error("Ready() = true, want false (one plugin unhealthy)")
	}

	rs := rc.ReadinessStatus()
	if rs.PluginsReady {
		t.Error("PluginsReady = true, want false")
	}
	if rs.Plugins["tls"].Healthy != true {
		t.Error("plugins[tls].Healthy = false, want true")
	}
	if rs.Plugins["user-management"].Healthy != false {
		t.Error("plugins[user-management].Healthy = true, want false")
	}
}

func TestRegistry_ReadinessChecker_UpstreamHealthy_IsReady(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "tls", healthy: true})

	upstream := &stubUpstreamChecker{status: domainheal.StatusHealthy}
	rc := r.ReadinessChecker(upstream)

	if !rc.Ready() {
		t.Error("Ready() = false, want true (plugins healthy, upstream healthy)")
	}

	rs := rc.ReadinessStatus()
	if !rs.UpstreamReachable {
		t.Error("UpstreamReachable = false, want true")
	}
}

func TestRegistry_ReadinessChecker_UpstreamUnhealthy_NotReady(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "tls", healthy: true})

	upstream := &stubUpstreamChecker{status: domainheal.StatusUnhealthy}
	rc := r.ReadinessChecker(upstream)

	if rc.Ready() {
		t.Error("Ready() = true, want false (upstream unhealthy)")
	}

	rs := rc.ReadinessStatus()
	if rs.UpstreamReachable {
		t.Error("UpstreamReachable = true, want false")
	}
}

func TestRegistry_ReadinessChecker_UpstreamUnknown_NotReady(t *testing.T) {
	r := NewRegistry(slog.Default())

	upstream := &stubUpstreamChecker{status: domainheal.StatusUnknown}
	rc := r.ReadinessChecker(upstream)

	if rc.Ready() {
		t.Error("Ready() = true, want false (upstream status unknown)")
	}

	rs := rc.ReadinessStatus()
	if rs.UpstreamReachable {
		t.Error("UpstreamReachable = true, want false (unknown is not reachable)")
	}
}

func TestRegistry_ReadinessChecker_BothUnhealthy_NotReady(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "user-management", healthy: false})

	upstream := &stubUpstreamChecker{status: domainheal.StatusUnhealthy}
	rc := r.ReadinessChecker(upstream)

	if rc.Ready() {
		t.Error("Ready() = true, want false (plugin unhealthy and upstream unhealthy)")
	}

	rs := rc.ReadinessStatus()
	if rs.PluginsReady {
		t.Error("PluginsReady = true, want false")
	}
	if rs.UpstreamReachable {
		t.Error("UpstreamReachable = true, want false")
	}
}

func TestRegistry_ReadinessChecker_PluginsMapIncludesAllRegistered(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "alpha", healthy: true})
	r.Register(&stubPlugin{name: "beta", healthy: false})
	r.Register(&stubPlugin{name: "gamma", healthy: true})

	rc := r.ReadinessChecker(nil)
	rs := rc.ReadinessStatus()

	if len(rs.Plugins) != 3 {
		t.Errorf("len(Plugins) = %d, want 3", len(rs.Plugins))
	}
	if !rs.Plugins["alpha"].Healthy {
		t.Error("plugins[alpha].Healthy = false, want true")
	}
	if rs.Plugins["beta"].Healthy {
		t.Error("plugins[beta].Healthy = true, want false")
	}
	if !rs.Plugins["gamma"].Healthy {
		t.Error("plugins[gamma].Healthy = false, want true")
	}
}

func TestRegistry_ReadinessChecker_IsSafeForConcurrentUse(t *testing.T) {
	r := NewRegistry(slog.Default())
	r.Register(&stubPlugin{name: "tls", healthy: true})

	rc := r.ReadinessChecker(nil)

	// Run Ready() and ReadinessStatus() concurrently — must not race.
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			_ = rc.Ready()
			_ = rc.ReadinessStatus()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestRegistry_ReadinessChecker_ImplementsInterface ensures the returned value
// satisfies ports.ReadinessChecker at compile time.
func TestRegistry_ReadinessChecker_ImplementsInterface(t *testing.T) {
	r := NewRegistry(slog.Default())
	rc := r.ReadinessChecker(nil)

	// Compile-time assertion — Ready() must exist on rc.
	if rc == nil {
		t.Error("ReadinessChecker returned nil")
	}
}
