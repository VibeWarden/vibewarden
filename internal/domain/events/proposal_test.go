package events_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewProposalCreated(t *testing.T) {
	params := events.ProposalCreatedParams{
		ProposalID: "test-id-123",
		ActionType: "block_ip",
		Reason:     "too many requests",
		Source:     "mcp_agent",
	}

	ev := events.NewProposalCreated(params)

	if ev.EventType != events.EventTypeAgentProposalCreated {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAgentProposalCreated)
	}
	if ev.SchemaVersion != events.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
	}
	if !strings.Contains(ev.AISummary, "test-id-123") {
		t.Errorf("AISummary %q should contain proposal ID", ev.AISummary)
	}
	if !strings.Contains(ev.AISummary, "block_ip") {
		t.Errorf("AISummary %q should contain action type", ev.AISummary)
	}
	if ev.Payload["proposal_id"] != "test-id-123" {
		t.Errorf("Payload[proposal_id] = %v, want %q", ev.Payload["proposal_id"], "test-id-123")
	}
	if ev.Payload["action_type"] != "block_ip" {
		t.Errorf("Payload[action_type] = %v, want %q", ev.Payload["action_type"], "block_ip")
	}
	if ev.Payload["source"] != "mcp_agent" {
		t.Errorf("Payload[source] = %v, want %q", ev.Payload["source"], "mcp_agent")
	}
	if ev.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", ev.Actor.Type, events.ActorTypeSystem)
	}
	if ev.Resource.Type != events.ResourceTypeConfig {
		t.Errorf("Resource.Type = %q, want %q", ev.Resource.Type, events.ResourceTypeConfig)
	}
	if ev.Outcome != events.OutcomeAllowed {
		t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeAllowed)
	}
}

func TestNewProposalApproved(t *testing.T) {
	params := events.ProposalApprovedParams{
		ProposalID: "approved-id",
		ActionType: "adjust_rate_limit",
	}

	ev := events.NewProposalApproved(params)

	if ev.EventType != events.EventTypeAgentProposalApproved {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAgentProposalApproved)
	}
	if !strings.Contains(ev.AISummary, "approved-id") {
		t.Errorf("AISummary %q should contain proposal ID", ev.AISummary)
	}
	if ev.Payload["proposal_id"] != "approved-id" {
		t.Errorf("Payload[proposal_id] = %v, want %q", ev.Payload["proposal_id"], "approved-id")
	}
	if ev.Outcome != events.OutcomeAllowed {
		t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeAllowed)
	}
}

func TestNewProposalDismissed(t *testing.T) {
	params := events.ProposalDismissedParams{
		ProposalID: "dismissed-id",
		ActionType: "block_ip",
	}

	ev := events.NewProposalDismissed(params)

	if ev.EventType != events.EventTypeAgentProposalDismissed {
		t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeAgentProposalDismissed)
	}
	if !strings.Contains(ev.AISummary, "dismissed-id") {
		t.Errorf("AISummary %q should contain proposal ID", ev.AISummary)
	}
	if ev.Payload["proposal_id"] != "dismissed-id" {
		t.Errorf("Payload[proposal_id] = %v, want %q", ev.Payload["proposal_id"], "dismissed-id")
	}
	if ev.Outcome != events.OutcomeBlocked {
		t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeBlocked)
	}
}
