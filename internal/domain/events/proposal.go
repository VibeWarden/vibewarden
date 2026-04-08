package events

import (
	"fmt"
	"time"
)

// Proposal lifecycle event type constants.
const (
	// EventTypeAgentProposalCreated is emitted when an MCP agent creates a new
	// configuration-change proposal. The proposal is pending human review.
	EventTypeAgentProposalCreated = "agent.proposal_created"

	// EventTypeAgentProposalApproved is emitted when a human admin approves a
	// pending proposal and the configuration change is applied.
	EventTypeAgentProposalApproved = "agent.proposal_approved"

	// EventTypeAgentProposalDismissed is emitted when a human admin dismisses a
	// pending proposal without applying the change.
	EventTypeAgentProposalDismissed = "agent.proposal_dismissed"
)

// ProposalCreatedParams holds the parameters needed to construct an
// agent.proposal_created event.
type ProposalCreatedParams struct {
	// ProposalID is the UUID of the newly created proposal.
	ProposalID string

	// ActionType is the kind of configuration change (e.g. "block_ip").
	ActionType string

	// Reason is the agent's justification for the proposal.
	Reason string

	// Source identifies the component that created the proposal (e.g. "mcp_agent").
	Source string
}

// NewProposalCreated creates an agent.proposal_created event.
func NewProposalCreated(params ProposalCreatedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAgentProposalCreated,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Proposal %s created by %s: %s — %s",
			params.ProposalID, params.Source, params.ActionType, params.Reason,
		),
		Payload: map[string]any{
			"proposal_id": params.ProposalID,
			"action_type": params.ActionType,
			"reason":      params.Reason,
			"source":      params.Source,
		},
		Actor:       Actor{Type: ActorTypeSystem, ID: params.Source},
		Resource:    Resource{Type: ResourceTypeConfig},
		Outcome:     OutcomeAllowed,
		TriggeredBy: params.Source,
	}
}

// ProposalApprovedParams holds the parameters needed to construct an
// agent.proposal_approved event.
type ProposalApprovedParams struct {
	// ProposalID is the UUID of the approved proposal.
	ProposalID string

	// ActionType is the kind of configuration change that was applied.
	ActionType string
}

// NewProposalApproved creates an agent.proposal_approved event.
func NewProposalApproved(params ProposalApprovedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAgentProposalApproved,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Proposal %s approved and applied: %s",
			params.ProposalID, params.ActionType,
		),
		Payload: map[string]any{
			"proposal_id": params.ProposalID,
			"action_type": params.ActionType,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig},
		Outcome:     OutcomeAllowed,
		TriggeredBy: "admin_api",
	}
}

// ProposalDismissedParams holds the parameters needed to construct an
// agent.proposal_dismissed event.
type ProposalDismissedParams struct {
	// ProposalID is the UUID of the dismissed proposal.
	ProposalID string

	// ActionType is the kind of configuration change that was dismissed.
	ActionType string
}

// NewProposalDismissed creates an agent.proposal_dismissed event.
func NewProposalDismissed(params ProposalDismissedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeAgentProposalDismissed,
		Timestamp:     time.Now().UTC(),
		AISummary: fmt.Sprintf(
			"Proposal %s dismissed: %s",
			params.ProposalID, params.ActionType,
		),
		Payload: map[string]any{
			"proposal_id": params.ProposalID,
			"action_type": params.ActionType,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig},
		Outcome:     OutcomeBlocked,
		TriggeredBy: "admin_api",
	}
}
