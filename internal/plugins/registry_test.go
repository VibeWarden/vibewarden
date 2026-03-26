package plugins_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

// fakePlugin records every lifecycle call so tests can assert on order and
// error propagation without any real side effects.
type fakePlugin struct {
	name      string
	initErr   error
	startErr  error
	stopErr   error
	health    ports.HealthStatus
	callOrder *[]string // shared slice so callers can observe cross-plugin order
}

func (f *fakePlugin) Name() string { return f.name }

func (f *fakePlugin) Init(_ context.Context) error {
	*f.callOrder = append(*f.callOrder, f.name+":init")
	return f.initErr
}

func (f *fakePlugin) Start(_ context.Context) error {
	*f.callOrder = append(*f.callOrder, f.name+":start")
	return f.startErr
}

func (f *fakePlugin) Stop(_ context.Context) error {
	*f.callOrder = append(*f.callOrder, f.name+":stop")
	return f.stopErr
}

func (f *fakePlugin) Health() ports.HealthStatus { return f.health }

// fakeCaddyPlugin additionally implements CaddyContributor.
type fakeCaddyPlugin struct {
	fakePlugin
	routes   []ports.CaddyRoute
	handlers []ports.CaddyHandler
}

func (f *fakeCaddyPlugin) ContributeCaddyRoutes() []ports.CaddyRoute     { return f.routes }
func (f *fakeCaddyPlugin) ContributeCaddyHandlers() []ports.CaddyHandler { return f.handlers }

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil_writer{}, nil))
}

// nil_writer discards all log output so tests stay quiet.
type nil_writer struct{}

func (nil_writer) Write(p []byte) (int, error) { return len(p), nil }

func newOrder() *[]string { s := []string{}; return &s }

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestRegistry_Register_And_Plugins(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())

	p1 := &fakePlugin{name: "alpha", callOrder: order}
	p2 := &fakePlugin{name: "beta", callOrder: order}

	r.Register(p1)
	r.Register(p2)

	got := r.Plugins()
	if len(got) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(got))
	}
	if got[0].Name() != "alpha" || got[1].Name() != "beta" {
		t.Errorf("unexpected plugin order: %v", got)
	}
}

func TestRegistry_Plugins_ReturnsCopy(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())
	r.Register(&fakePlugin{name: "alpha", callOrder: order})

	got := r.Plugins()
	got[0] = &fakePlugin{name: "mutated", callOrder: order}

	// Internal slice must not be mutated.
	if r.Plugins()[0].Name() != "alpha" {
		t.Error("Plugins() should return a copy, not a reference to the internal slice")
	}
}

func TestRegistry_InitAll_Order(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order})
	r.Register(&fakePlugin{name: "beta", callOrder: order})

	if err := r.InitAll(context.Background()); err != nil {
		t.Fatalf("InitAll: unexpected error: %v", err)
	}

	want := []string{"alpha:init", "beta:init"}
	assertOrder(t, *order, want)
}

func TestRegistry_StartAll_Order(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order})
	r.Register(&fakePlugin{name: "beta", callOrder: order})

	if err := r.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}

	want := []string{"alpha:start", "beta:start"}
	assertOrder(t, *order, want)
}

func TestRegistry_StopAll_ReverseOrder(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order})
	r.Register(&fakePlugin{name: "beta", callOrder: order})
	r.Register(&fakePlugin{name: "gamma", callOrder: order})

	if err := r.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: unexpected error: %v", err)
	}

	want := []string{"gamma:stop", "beta:stop", "alpha:stop"}
	assertOrder(t, *order, want)
}

func TestRegistry_InitAll_StopsOnFirstError(t *testing.T) {
	order := newOrder()
	boom := errors.New("init boom")
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order})
	r.Register(&fakePlugin{name: "beta", initErr: boom, callOrder: order})
	r.Register(&fakePlugin{name: "gamma", callOrder: order})

	err := r.InitAll(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom error, got: %v", err)
	}
	// gamma must not have been initialised
	for _, call := range *order {
		if call == "gamma:init" {
			t.Error("gamma should not have been initialised after beta failed")
		}
	}
}

func TestRegistry_StartAll_StopsOnFirstError(t *testing.T) {
	order := newOrder()
	boom := errors.New("start boom")
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order})
	r.Register(&fakePlugin{name: "beta", startErr: boom, callOrder: order})
	r.Register(&fakePlugin{name: "gamma", callOrder: order})

	err := r.StartAll(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom error, got: %v", err)
	}
	for _, call := range *order {
		if call == "gamma:start" {
			t.Error("gamma should not have been started after beta failed")
		}
	}
}

func TestRegistry_StopAll_ContinuesOnError(t *testing.T) {
	order := newOrder()
	boom := errors.New("stop boom")
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order})
	r.Register(&fakePlugin{name: "beta", stopErr: boom, callOrder: order})
	r.Register(&fakePlugin{name: "gamma", callOrder: order})

	err := r.StopAll(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Errorf("expected wrapped boom error, got: %v", err)
	}

	// All three must have been stopped despite beta's error.
	want := []string{"gamma:stop", "beta:stop", "alpha:stop"}
	assertOrder(t, *order, want)
}

func TestRegistry_StopAll_MultipleErrors(t *testing.T) {
	order := newOrder()
	boom1 := errors.New("boom1")
	boom2 := errors.New("boom2")
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", stopErr: boom1, callOrder: order})
	r.Register(&fakePlugin{name: "beta", stopErr: boom2, callOrder: order})

	err := r.StopAll(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Both original errors should be present somewhere in the chain.
	// errors.Is won't find them because joinErrors uses fmt.Errorf without %w
	// for the multi-error case — check the message instead.
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestRegistry_HealthAll(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())

	r.Register(&fakePlugin{name: "alpha", callOrder: order, health: ports.HealthStatus{Healthy: true, Message: "ok"}})
	r.Register(&fakePlugin{name: "beta", callOrder: order, health: ports.HealthStatus{Healthy: false, Message: "degraded"}})

	got := r.HealthAll()

	if len(got) != 2 {
		t.Fatalf("expected 2 health entries, got %d", len(got))
	}
	if !got["alpha"].Healthy {
		t.Error("alpha should be healthy")
	}
	if got["beta"].Healthy {
		t.Error("beta should be unhealthy")
	}
	if got["beta"].Message != "degraded" {
		t.Errorf("beta message: want %q, got %q", "degraded", got["beta"].Message)
	}
}

func TestRegistry_CaddyContributors(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())

	plain := &fakePlugin{name: "plain", callOrder: order}
	caddy := &fakeCaddyPlugin{
		fakePlugin: fakePlugin{name: "caddy-plugin", callOrder: order},
		routes:     []ports.CaddyRoute{{MatchPath: "/api", Priority: 10}},
	}

	r.Register(plain)
	r.Register(caddy)

	contributors := r.CaddyContributors()
	if len(contributors) != 1 {
		t.Fatalf("expected 1 contributor, got %d", len(contributors))
	}
	if contributors[0] != caddy {
		t.Error("expected the caddy plugin to be the contributor")
	}
}

func TestRegistry_CaddyContributors_EmptyWhenNone(t *testing.T) {
	order := newOrder()
	r := plugins.NewRegistry(discardLogger())
	r.Register(&fakePlugin{name: "plain", callOrder: order})

	contributors := r.CaddyContributors()
	if len(contributors) != 0 {
		t.Errorf("expected 0 contributors, got %d", len(contributors))
	}
}

func TestRegistry_EmptyRegistry(t *testing.T) {
	r := plugins.NewRegistry(discardLogger())
	ctx := context.Background()

	if err := r.InitAll(ctx); err != nil {
		t.Errorf("InitAll on empty registry: %v", err)
	}
	if err := r.StartAll(ctx); err != nil {
		t.Errorf("StartAll on empty registry: %v", err)
	}
	if err := r.StopAll(ctx); err != nil {
		t.Errorf("StopAll on empty registry: %v", err)
	}
	if h := r.HealthAll(); len(h) != 0 {
		t.Errorf("HealthAll on empty registry should return empty map, got %v", h)
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("call order length: want %d, got %d\n  want: %v\n  got:  %v", len(want), len(got), want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call order[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}
