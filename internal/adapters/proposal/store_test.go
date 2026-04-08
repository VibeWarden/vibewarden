package proposal_test

import (
	"context"
	"testing"
	"time"

	proposaladapter "github.com/vibewarden/vibewarden/internal/adapters/proposal"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

func makeProposal(id string, now time.Time) proposal.Proposal {
	return proposal.Proposal{
		ID:        id,
		Type:      proposal.ActionBlockIP,
		Params:    map[string]any{"ip": "1.2.3.4"},
		Reason:    "test reason",
		Status:    proposal.StatusPending,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
		Source:    proposal.SourceMCPAgent,
	}
}

func TestStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()

	now := time.Now()
	p := makeProposal("p1", now)

	created, err := store.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if created.ID != "p1" {
		t.Errorf("created.ID = %q, want %q", created.ID, "p1")
	}

	got, err := store.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if got.ID != "p1" {
		t.Errorf("got.ID = %q, want %q", got.ID, "p1")
	}
	if got.Status != proposal.StatusPending {
		t.Errorf("got.Status = %q, want %q", got.Status, proposal.StatusPending)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()

	_, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Get: expected ErrProposalNotFound, got nil")
	}
	if !isErrProposalNotFound(err) {
		t.Errorf("Get error = %v, want ErrProposalNotFound", err)
	}
}

func TestStore_List(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	now := time.Now()

	p1 := makeProposal("p1", now)
	p2 := makeProposal("p2", now)
	p2.Status = proposal.StatusApproved

	_, _ = store.Create(ctx, p1)
	_, _ = store.Create(ctx, p2)

	all, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List all: unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("List all: got %d proposals, want 2", len(all))
	}

	pending, err := store.List(ctx, proposal.StatusPending)
	if err != nil {
		t.Fatalf("List pending: unexpected error: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "p1" {
		t.Errorf("List pending: got %v, want [{ID:p1}]", pending)
	}
}

func TestStore_Approve(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	now := time.Now()

	p := makeProposal("p1", now)
	_, _ = store.Create(ctx, p)

	approved, err := store.Approve(ctx, "p1")
	if err != nil {
		t.Fatalf("Approve: unexpected error: %v", err)
	}
	if approved.Status != proposal.StatusApproved {
		t.Errorf("approved.Status = %q, want %q", approved.Status, proposal.StatusApproved)
	}

	// Cannot approve twice.
	_, err = store.Approve(ctx, "p1")
	if err == nil {
		t.Fatal("Approve again: expected ErrProposalNotPending, got nil")
	}
}

func TestStore_Dismiss(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()
	now := time.Now()

	p := makeProposal("p1", now)
	_, _ = store.Create(ctx, p)

	dismissed, err := store.Dismiss(ctx, "p1")
	if err != nil {
		t.Fatalf("Dismiss: unexpected error: %v", err)
	}
	if dismissed.Status != proposal.StatusDismissed {
		t.Errorf("dismissed.Status = %q, want %q", dismissed.Status, proposal.StatusDismissed)
	}

	// Cannot dismiss again.
	_, err = store.Dismiss(ctx, "p1")
	if err == nil {
		t.Fatal("Dismiss again: expected ErrProposalNotPending, got nil")
	}
}

func TestStore_ApproveNotFound(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()

	_, err := store.Approve(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Approve nonexistent: expected ErrProposalNotFound, got nil")
	}
	if !isErrProposalNotFound(err) {
		t.Errorf("Approve nonexistent error = %v, want ErrProposalNotFound", err)
	}
}

func TestStore_DismissNotFound(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()

	_, err := store.Dismiss(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Dismiss nonexistent: expected ErrProposalNotFound, got nil")
	}
}

func TestStore_ExpiredProposalOnGet(t *testing.T) {
	ctx := context.Background()

	store := proposaladapter.NewStoreWithClock(func() time.Time {
		return time.Now().Add(-2 * time.Hour) // clock is in the past during creation
	})

	now := time.Now()
	p := proposal.Proposal{
		ID:        "p1",
		Type:      proposal.ActionBlockIP,
		Params:    map[string]any{"ip": "1.2.3.4"},
		Reason:    "test",
		Status:    proposal.StatusPending,
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour), // already expired
		Source:    proposal.SourceMCPAgent,
	}
	_, _ = store.Create(ctx, p)

	// Use a store with a real clock for the get so the expiry check triggers.
	store2 := proposaladapter.NewStore()
	_, _ = store2.Create(ctx, p)

	got, err := store2.Get(ctx, "p1")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	if got.Status != proposal.StatusExpired {
		t.Errorf("get status = %q, want %q", got.Status, proposal.StatusExpired)
	}
}

func TestStore_ExpiredProposalCannotBeApproved(t *testing.T) {
	ctx := context.Background()
	store := proposaladapter.NewStore()

	now := time.Now()
	p := proposal.Proposal{
		ID:        "p1",
		Type:      proposal.ActionBlockIP,
		Params:    map[string]any{"ip": "1.2.3.4"},
		Reason:    "test",
		Status:    proposal.StatusPending,
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Second),
		Source:    proposal.SourceMCPAgent,
	}
	_, _ = store.Create(ctx, p)

	_, err := store.Approve(ctx, "p1")
	if err == nil {
		t.Fatal("Approve expired: expected ErrProposalNotPending, got nil")
	}
}

// isErrProposalNotFound checks whether err wraps or equals ErrProposalNotFound.
func isErrProposalNotFound(err error) bool {
	return err != nil && err.Error() == ports.ErrProposalNotFound.Error()
}
