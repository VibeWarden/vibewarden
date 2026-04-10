package ports

import (
	"context"
	"errors"

	"github.com/vibewarden/vibewarden/internal/domain/proposal"
)

// ErrProposalNotFound is returned when a proposal with the given ID does not exist.
var ErrProposalNotFound = errors.New("proposal not found")

// ErrProposalNotPending is returned when an action (approve or dismiss) is
// attempted on a proposal that is not in the pending state.
var ErrProposalNotPending = errors.New("proposal is not pending")

// ProposalStore is the port for persisting and retrieving proposals.
// The in-memory adapter implements this interface; future adapters could back
// it with a database.
type ProposalStore interface {
	// Create persists a new proposal and returns the stored value.
	Create(ctx context.Context, p proposal.Proposal) (proposal.Proposal, error)

	// List returns proposals filtered by status. An empty status string returns
	// all proposals. Expired proposals are auto-transitioned to StatusExpired on
	// access.
	List(ctx context.Context, status proposal.Status) ([]proposal.Proposal, error)

	// Get returns the proposal with the given ID.
	// Returns ErrProposalNotFound when no proposal with that ID exists.
	Get(ctx context.Context, id string) (proposal.Proposal, error)

	// Approve transitions the proposal to StatusApproved.
	// Returns ErrProposalNotFound when no proposal with that ID exists.
	// Returns ErrProposalNotPending when the proposal is not in StatusPending.
	Approve(ctx context.Context, id string) (proposal.Proposal, error)

	// Dismiss transitions the proposal to StatusDismissed.
	// Returns ErrProposalNotFound when no proposal with that ID exists.
	// Returns ErrProposalNotPending when the proposal is not in StatusPending.
	Dismiss(ctx context.Context, id string) (proposal.Proposal, error)
}

// ProposalApplier applies approved proposals to the live configuration.
// The reload service implements this interface.
type ProposalApplier interface {
	// Apply applies the configuration change described by p and triggers a
	// config reload. It must write the updated config to disk before reloading
	// so that the change survives restarts.
	Apply(ctx context.Context, p proposal.Proposal) error
}
