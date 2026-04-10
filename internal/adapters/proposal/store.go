// Package proposal provides an in-memory implementation of ports.ProposalStore.
// Proposals are stored in a sync.RWMutex-protected map; expired proposals are
// auto-transitioned to StatusExpired on access. No persistence — if the sidecar
// restarts all pending proposals are lost (by design for the v1 in-memory store).
package proposal

import (
	"context"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Store is an in-memory implementation of ports.ProposalStore.
type Store struct {
	mu    sync.RWMutex
	items map[string]proposal.Proposal
	now   func() time.Time // injectable for testing
}

// NewStore creates an empty in-memory Store.
func NewStore() *Store {
	return &Store{
		items: make(map[string]proposal.Proposal),
		now:   time.Now,
	}
}

// NewStoreWithClock creates an empty in-memory Store with a custom clock
// function. Intended for use in tests only.
func NewStoreWithClock(clock func() time.Time) *Store {
	return &Store{
		items: make(map[string]proposal.Proposal),
		now:   clock,
	}
}

// Create implements ports.ProposalStore.Create.
func (s *Store) Create(_ context.Context, p proposal.Proposal) (proposal.Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[p.ID] = p
	return p, nil
}

// List implements ports.ProposalStore.List.
// Expired proposals are transitioned to StatusExpired before filtering.
// An empty status string returns all proposals.
func (s *Store) List(_ context.Context, status proposal.Status) ([]proposal.Proposal, error) {
	now := s.now()

	s.mu.Lock()
	s.expireAll(now)
	s.mu.Unlock()

	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []proposal.Proposal
	for _, p := range s.items {
		if status == "" || p.Status == status {
			out = append(out, p)
		}
	}
	return out, nil
}

// Get implements ports.ProposalStore.Get.
// If the proposal is expired it is transitioned to StatusExpired before
// returning.
func (s *Store) Get(_ context.Context, id string) (proposal.Proposal, error) {
	now := s.now()

	s.mu.Lock()
	p, ok := s.items[id]
	if !ok {
		s.mu.Unlock()
		return proposal.Proposal{}, ports.ErrProposalNotFound
	}
	if p.Status == proposal.StatusPending && p.IsExpired(now) {
		p.Status = proposal.StatusExpired
		s.items[id] = p
	}
	s.mu.Unlock()

	return p, nil
}

// Approve implements ports.ProposalStore.Approve.
func (s *Store) Approve(ctx context.Context, id string) (proposal.Proposal, error) {
	return s.transition(ctx, id, proposal.StatusApproved)
}

// Dismiss implements ports.ProposalStore.Dismiss.
func (s *Store) Dismiss(ctx context.Context, id string) (proposal.Proposal, error) {
	return s.transition(ctx, id, proposal.StatusDismissed)
}

// transition updates the status of the proposal identified by id to next.
// It returns ErrProposalNotFound when the proposal does not exist and
// ErrProposalNotPending when it is not in the pending state.
func (s *Store) transition(_ context.Context, id string, next proposal.Status) (proposal.Proposal, error) {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.items[id]
	if !ok {
		return proposal.Proposal{}, ports.ErrProposalNotFound
	}

	// Expire on access before checking pending.
	if p.Status == proposal.StatusPending && p.IsExpired(now) {
		p.Status = proposal.StatusExpired
		s.items[id] = p
	}

	if p.Status != proposal.StatusPending {
		return proposal.Proposal{}, ports.ErrProposalNotPending
	}

	p.Status = next
	s.items[id] = p
	return p, nil
}

// expireAll transitions all pending expired proposals. Must be called with the
// write lock held.
func (s *Store) expireAll(now time.Time) {
	for id, p := range s.items {
		if p.Status == proposal.StatusPending && p.IsExpired(now) {
			p.Status = proposal.StatusExpired
			s.items[id] = p
		}
	}
}
