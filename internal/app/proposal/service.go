// Package proposal provides the application service for the propose-action workflow.
// It orchestrates proposal creation, listing, approval, and dismissal. Business
// logic lives in the domain package; this service coordinates ports only.
package proposal

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates the proposal lifecycle.
type Service struct {
	store    ports.ProposalStore
	applier  ports.ProposalApplier
	eventLog ports.EventLogger
	logger   *slog.Logger
}

// NewService creates a Service wired to the given store and applier.
// eventLog and logger may be nil; they are used for audit events and
// diagnostics respectively.
func NewService(
	store ports.ProposalStore,
	applier ports.ProposalApplier,
	eventLog ports.EventLogger,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:    store,
		applier:  applier,
		eventLog: eventLog,
		logger:   logger,
	}
}

// CreateParams holds the inputs required to create a new proposal.
type CreateParams struct {
	// Type is the action type (block_ip, adjust_rate_limit, update_config).
	Type proposal.ActionType

	// Params holds action-specific parameters.
	Params map[string]any

	// Reason is the agent's justification for the proposal.
	Reason string

	// Source identifies the creating component (typically "mcp_agent").
	Source string

	// TTL overrides the default proposal TTL. Zero means DefaultTTL (1 hour).
	TTL time.Duration
}

// Create creates a new pending proposal, persists it, and emits an audit event.
func (s *Service) Create(ctx context.Context, p CreateParams) (proposal.Proposal, error) {
	ttl := p.TTL
	if ttl <= 0 {
		ttl = proposal.DefaultTTL
	}

	source := p.Source
	if source == "" {
		source = proposal.SourceMCPAgent
	}

	now := time.Now().UTC()
	prop := proposal.Proposal{
		ID:        uuid.NewString(),
		Type:      p.Type,
		Params:    p.Params,
		Reason:    p.Reason,
		Status:    proposal.StatusPending,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Source:    source,
	}

	created, err := s.store.Create(ctx, prop)
	if err != nil {
		return proposal.Proposal{}, fmt.Errorf("creating proposal: %w", err)
	}

	s.emitCreated(ctx, created)
	return created, nil
}

// List returns proposals filtered by status. An empty status returns all proposals.
func (s *Service) List(ctx context.Context, status proposal.Status) ([]proposal.Proposal, error) {
	proposals, err := s.store.List(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("listing proposals: %w", err)
	}
	return proposals, nil
}

// Get returns the proposal with the given ID.
func (s *Service) Get(ctx context.Context, id string) (proposal.Proposal, error) {
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return proposal.Proposal{}, fmt.Errorf("getting proposal: %w", err)
	}
	return p, nil
}

// Approve transitions the proposal to approved, applies the config change, and
// emits an audit event.
func (s *Service) Approve(ctx context.Context, id string) (proposal.Proposal, error) {
	p, err := s.store.Approve(ctx, id)
	if err != nil {
		return proposal.Proposal{}, fmt.Errorf("approving proposal: %w", err)
	}

	if err := s.applier.Apply(ctx, p); err != nil {
		s.logger.Error("applying proposal failed",
			slog.String("proposal_id", id),
			slog.String("action_type", string(p.Type)),
			slog.String("error", err.Error()),
		)
		return proposal.Proposal{}, fmt.Errorf("applying proposal: %w", err)
	}

	s.emitApproved(ctx, p)
	return p, nil
}

// Dismiss transitions the proposal to dismissed and emits an audit event.
func (s *Service) Dismiss(ctx context.Context, id string) (proposal.Proposal, error) {
	p, err := s.store.Dismiss(ctx, id)
	if err != nil {
		return proposal.Proposal{}, fmt.Errorf("dismissing proposal: %w", err)
	}

	s.emitDismissed(ctx, p)
	return p, nil
}

// ------------------------------------------------------------------
// Event helpers
// ------------------------------------------------------------------

func (s *Service) emitCreated(ctx context.Context, p proposal.Proposal) {
	if s.eventLog == nil {
		return
	}
	ev := events.NewProposalCreated(events.ProposalCreatedParams{
		ProposalID: p.ID,
		ActionType: string(p.Type),
		Reason:     p.Reason,
		Source:     p.Source,
	})
	if err := s.eventLog.Log(ctx, ev); err != nil {
		s.logger.Error("failed to emit proposal_created event", slog.String("error", err.Error()))
	}
}

func (s *Service) emitApproved(ctx context.Context, p proposal.Proposal) {
	if s.eventLog == nil {
		return
	}
	ev := events.NewProposalApproved(events.ProposalApprovedParams{
		ProposalID: p.ID,
		ActionType: string(p.Type),
	})
	if err := s.eventLog.Log(ctx, ev); err != nil {
		s.logger.Error("failed to emit proposal_approved event", slog.String("error", err.Error()))
	}
}

func (s *Service) emitDismissed(ctx context.Context, p proposal.Proposal) {
	if s.eventLog == nil {
		return
	}
	ev := events.NewProposalDismissed(events.ProposalDismissedParams{
		ProposalID: p.ID,
		ActionType: string(p.Type),
	})
	if err := s.eventLog.Log(ctx, ev); err != nil {
		s.logger.Error("failed to emit proposal_dismissed event", slog.String("error", err.Error()))
	}
}
