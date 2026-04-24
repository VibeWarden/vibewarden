package caddy

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeEventLoggerSvc is a test double for ports.EventLogger.
type fakeEventLoggerSvc struct {
	logged []events.Event
	mu     sync.Mutex
}

func (f *fakeEventLoggerSvc) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	f.logged = append(f.logged, ev)
	f.mu.Unlock()
	return nil
}

// fakeAuditLoggerSvc is a test double for ports.AuditEventLogger.
type fakeAuditLoggerSvc struct {
	logged []audit.AuditEvent
	mu     sync.Mutex
}

func (f *fakeAuditLoggerSvc) Log(_ context.Context, ev audit.AuditEvent) error {
	f.mu.Lock()
	f.logged = append(f.logged, ev)
	f.mu.Unlock()
	return nil
}

// TestRuntimeServices_ZeroValueWhenUnset asserts that currentServices returns a
// zero-value RuntimeServices when SetRuntimeServices has never been called.
func TestRuntimeServices_ZeroValueWhenUnset(t *testing.T) {
	// Reset the registry to nil before this test so other tests don't affect it.
	runtimeServicesRegistry.Store(nil)

	svc := currentServices()
	if svc.Logger != nil {
		t.Error("Logger should be nil in zero-value RuntimeServices")
	}
	if svc.EventLogger != nil {
		t.Error("EventLogger should be nil in zero-value RuntimeServices")
	}
	if svc.AuditEventLogger != nil {
		t.Error("AuditEventLogger should be nil in zero-value RuntimeServices")
	}
	if svc.RateLimiterFactory != nil {
		t.Error("RateLimiterFactory should be nil in zero-value RuntimeServices")
	}
	if svc.CircuitBreakerFactory != nil {
		t.Error("CircuitBreakerFactory should be nil in zero-value RuntimeServices")
	}
}

// TestRuntimeServices_SetAndGet verifies that SetRuntimeServices stores a value
// that is retrievable via currentServices.
func TestRuntimeServices_SetAndGet(t *testing.T) {
	logger := slog.Default()
	el := &fakeEventLoggerSvc{}
	al := &fakeAuditLoggerSvc{}

	SetRuntimeServices(RuntimeServices{
		Logger:           logger,
		EventLogger:      el,
		AuditEventLogger: al,
	})
	defer runtimeServicesRegistry.Store(nil) // cleanup

	got := currentServices()
	if got.Logger != logger {
		t.Error("Logger not stored correctly")
	}
	if got.EventLogger != el {
		t.Error("EventLogger not stored correctly")
	}
	if got.AuditEventLogger != al {
		t.Error("AuditEventLogger not stored correctly")
	}
}

// TestRuntimeServices_LastWriteWins verifies that concurrent SetRuntimeServices
// calls are both safe and result in a valid services value being readable.
func TestRuntimeServices_LastWriteWins(t *testing.T) {
	const goroutines = 50

	el1 := &fakeEventLoggerSvc{}
	el2 := &fakeEventLoggerSvc{}

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				SetRuntimeServices(RuntimeServices{EventLogger: el1})
			} else {
				SetRuntimeServices(RuntimeServices{EventLogger: el2})
			}
		}()
	}
	wg.Wait()
	defer runtimeServicesRegistry.Store(nil) // cleanup

	got := currentServices()
	// The result must be one of the two valid values — either el1 or el2.
	if got.EventLogger != el1 && got.EventLogger != el2 {
		t.Errorf("currentServices().EventLogger is neither el1 nor el2: got %v", got.EventLogger)
	}
}

// TestRuntimeServices_Idempotent verifies that calling SetRuntimeServices twice
// with different values results in the second value being visible.
func TestRuntimeServices_Idempotent(t *testing.T) {
	el1 := &fakeEventLoggerSvc{}
	el2 := &fakeEventLoggerSvc{}

	SetRuntimeServices(RuntimeServices{EventLogger: el1})
	SetRuntimeServices(RuntimeServices{EventLogger: el2})
	defer runtimeServicesRegistry.Store(nil) // cleanup

	got := currentServices()
	if got.EventLogger != el2 {
		t.Error("second SetRuntimeServices should overwrite the first")
	}
}

// TestSetRuntimeServices_CompileTimeInterfaceGuard verifies that the types in
// RuntimeServices satisfy the declared ports at compile time.
func TestSetRuntimeServices_CompileTimeInterfaceGuard(t *testing.T) {
	var _ ports.EventLogger = (*fakeEventLoggerSvc)(nil)
	var _ ports.AuditEventLogger = (*fakeAuditLoggerSvc)(nil)
}
