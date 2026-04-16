package ports

import (
	"context"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/proposal"
)

// ProposalCreateParams holds the inputs required to create a new proposal
// through ProposalService.Create. It is the port-owned equivalent of the
// application-layer CreateParams struct and is shaped identically so the app
// service can alias it without conversion.
type ProposalCreateParams struct {
	// Type is the action type (block_ip, adjust_rate_limit, update_config).
	Type proposal.ActionType

	// Params holds action-specific parameters.
	Params map[string]any

	// Reason is the agent's justification for the proposal.
	Reason string

	// Source identifies the creating component (typically "mcp_agent").
	Source string

	// TTL overrides the default proposal TTL. Zero means DefaultTTL.
	TTL time.Duration
}

// ProposalService is the inbound port exposed to HTTP/MCP adapters for the
// propose-action workflow. It aggregates the full create/list/get/approve/
// dismiss surface because the HTTP handler — today the only consumer —
// exercises every operation; splitting would force shim boilerplate without
// any ISP benefit.
//
// *proposal.Service in internal/app/proposal satisfies this port.
type ProposalService interface {
	// Create creates a new pending proposal, persists it, and emits an audit
	// event.
	Create(ctx context.Context, params ProposalCreateParams) (proposal.Proposal, error)

	// List returns proposals filtered by status. An empty status returns all
	// proposals.
	List(ctx context.Context, status proposal.Status) ([]proposal.Proposal, error)

	// Get returns the proposal with the given ID. Returns ErrProposalNotFound
	// when no proposal with that ID exists.
	Get(ctx context.Context, id string) (proposal.Proposal, error)

	// Approve transitions the proposal to approved, applies the config change,
	// and emits an audit event.
	Approve(ctx context.Context, id string) (proposal.Proposal, error)

	// Dismiss transitions the proposal to dismissed and emits an audit event.
	Dismiss(ctx context.Context, id string) (proposal.Proposal, error)
}
