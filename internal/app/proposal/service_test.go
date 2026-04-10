package proposal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	proposaladapter "github.com/vibewarden/vibewarden/internal/adapters/proposal"
	proposalapp "github.com/vibewarden/vibewarden/internal/app/proposal"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ------------------------------------------------------------------
// Fakes
// ------------------------------------------------------------------

// fakeApplier is a test fake for ports.ProposalApplier.
type fakeApplier struct {
	applied []proposal.Proposal
	err     error
}

func (a *fakeApplier) Apply(_ context.Context, p proposal.Proposal) error {
	if a.err != nil {
		return a.err
	}
	a.applied = append(a.applied, p)
	return nil
}

// fakeEventLogger records emitted events.
type fakeEventLogger struct {
	events []events.Event
}

func (l *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	l.events = append(l.events, ev)
	return nil
}

// ------------------------------------------------------------------
// Tests
// ------------------------------------------------------------------

func TestService_Create(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	applier := &fakeApplier{}
	logger := &fakeEventLogger{}

	svc := proposalapp.NewService(store, applier, logger, nil)

	p, err := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "1.2.3.4"},
		Reason: "high request rate",
		Source: proposal.SourceMCPAgent,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if p.ID == "" {
		t.Error("Create: ID should be set")
	}
	if p.Type != proposal.ActionBlockIP {
		t.Errorf("Create: Type = %q, want %q", p.Type, proposal.ActionBlockIP)
	}
	if p.Status != proposal.StatusPending {
		t.Errorf("Create: Status = %q, want %q", p.Status, proposal.StatusPending)
	}
	if p.ExpiresAt.Before(p.CreatedAt) {
		t.Error("Create: ExpiresAt must be after CreatedAt")
	}

	// Audit event should have been emitted.
	if len(logger.events) != 1 {
		t.Fatalf("Create: expected 1 event, got %d", len(logger.events))
	}
	if logger.events[0].EventType != events.EventTypeAgentProposalCreated {
		t.Errorf("Create event type = %q, want %q", logger.events[0].EventType, events.EventTypeAgentProposalCreated)
	}
}

func TestService_Create_DefaultTTL(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	svc := proposalapp.NewService(store, &fakeApplier{}, nil, nil)

	before := time.Now()
	p, err := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "10.0.0.1"},
		Reason: "test",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	after := time.Now()

	expectedExpiry := before.Add(proposal.DefaultTTL)
	if p.ExpiresAt.Before(expectedExpiry) {
		t.Errorf("ExpiresAt %v is before expected %v", p.ExpiresAt, expectedExpiry)
	}
	if p.ExpiresAt.After(after.Add(proposal.DefaultTTL)) {
		t.Errorf("ExpiresAt %v is too far in the future", p.ExpiresAt)
	}
}

func TestService_Create_CustomTTL(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	svc := proposalapp.NewService(store, &fakeApplier{}, nil, nil)

	ttl := 30 * time.Minute
	p, err := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "10.0.0.1"},
		Reason: "test",
		TTL:    ttl,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	delta := p.ExpiresAt.Sub(p.CreatedAt)
	if delta < ttl-time.Second || delta > ttl+time.Second {
		t.Errorf("TTL delta = %v, want ~%v", delta, ttl)
	}
}

func TestService_List(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	svc := proposalapp.NewService(store, &fakeApplier{}, nil, nil)

	for i := range 3 {
		_, err := svc.Create(ctx, proposalapp.CreateParams{
			Type:   proposal.ActionBlockIP,
			Params: map[string]any{"ip": "1.2.3." + string(rune('0'+i))},
			Reason: "test",
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	all, err := svc.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List all: got %d, want 3", len(all))
	}

	pending, err := svc.List(ctx, proposal.StatusPending)
	if err != nil {
		t.Fatalf("List pending: %v", err)
	}
	if len(pending) != 3 {
		t.Errorf("List pending: got %d, want 3", len(pending))
	}
}

func TestService_Get(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	svc := proposalapp.NewService(store, &fakeApplier{}, nil, nil)

	created, _ := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "5.5.5.5"},
		Reason: "test",
	})

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Get ID = %q, want %q", got.ID, created.ID)
	}

	// Not found.
	_, err = svc.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Get nonexistent: expected error")
	}
	if !errors.Is(err, ports.ErrProposalNotFound) {
		t.Errorf("Get nonexistent error = %v, want ErrProposalNotFound", err)
	}
}

func TestService_Approve(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	applier := &fakeApplier{}
	logger := &fakeEventLogger{}
	svc := proposalapp.NewService(store, applier, logger, nil)

	created, _ := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "9.9.9.9"},
		Reason: "test",
	})

	approved, err := svc.Approve(ctx, created.ID)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != proposal.StatusApproved {
		t.Errorf("Approve Status = %q, want %q", approved.Status, proposal.StatusApproved)
	}

	// Applier should have been called.
	if len(applier.applied) != 1 {
		t.Fatalf("Applier called %d times, want 1", len(applier.applied))
	}

	// Events: 1 created + 1 approved.
	eventTypes := make([]string, len(logger.events))
	for i, ev := range logger.events {
		eventTypes[i] = ev.EventType
	}
	if len(logger.events) != 2 {
		t.Fatalf("events count = %d, want 2; types: %v", len(logger.events), eventTypes)
	}
	if logger.events[1].EventType != events.EventTypeAgentProposalApproved {
		t.Errorf("second event type = %q, want %q", logger.events[1].EventType, events.EventTypeAgentProposalApproved)
	}
}

func TestService_Approve_ApplierError(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	applier := &fakeApplier{err: errors.New("write error")}
	svc := proposalapp.NewService(store, applier, nil, nil)

	created, _ := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionBlockIP,
		Params: map[string]any{"ip": "8.8.8.8"},
		Reason: "test",
	})

	_, err := svc.Approve(ctx, created.ID)
	if err == nil {
		t.Fatal("Approve: expected error from applier, got nil")
	}
}

func TestService_Dismiss(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	logger := &fakeEventLogger{}
	svc := proposalapp.NewService(store, &fakeApplier{}, logger, nil)

	created, _ := svc.Create(ctx, proposalapp.CreateParams{
		Type:   proposal.ActionUpdateConfig,
		Params: map[string]any{"log": map[string]any{"level": "debug"}},
		Reason: "verbose logging",
	})

	dismissed, err := svc.Dismiss(ctx, created.ID)
	if err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if dismissed.Status != proposal.StatusDismissed {
		t.Errorf("Dismiss Status = %q, want %q", dismissed.Status, proposal.StatusDismissed)
	}

	// Events: 1 created + 1 dismissed.
	if len(logger.events) != 2 {
		t.Fatalf("events count = %d, want 2", len(logger.events))
	}
	if logger.events[1].EventType != events.EventTypeAgentProposalDismissed {
		t.Errorf("second event type = %q, want %q", logger.events[1].EventType, events.EventTypeAgentProposalDismissed)
	}
}

func TestService_Dismiss_NotFound(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	svc := proposalapp.NewService(store, &fakeApplier{}, nil, nil)

	_, err := svc.Dismiss(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Dismiss nonexistent: expected error")
	}
	if !errors.Is(err, ports.ErrProposalNotFound) {
		t.Errorf("Dismiss nonexistent error = %v, want ErrProposalNotFound", err)
	}
}
