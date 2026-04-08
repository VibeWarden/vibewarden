// Package proposal defines the domain types for the propose-action workflow.
// An agent proposes a configuration change; a human approves or dismisses it;
// only on approval is the change applied.
//
// This package has zero external dependencies — only Go stdlib.
package proposal

import "time"

// Status represents the lifecycle state of a Proposal.
type Status string

const (
	// StatusPending means the proposal was created and is awaiting human review.
	StatusPending Status = "pending"

	// StatusApproved means a human approved the proposal. The config change
	// has been applied and a reload triggered.
	StatusApproved Status = "approved"

	// StatusDismissed means a human explicitly dismissed the proposal without
	// applying it.
	StatusDismissed Status = "dismissed"

	// StatusExpired means the proposal TTL elapsed before a human acted on it.
	StatusExpired Status = "expired"
)

// ActionType identifies what kind of configuration change the proposal describes.
type ActionType string

const (
	// ActionBlockIP adds an IP address or CIDR to the ip_filter blocklist.
	ActionBlockIP ActionType = "block_ip"

	// ActionAdjustRateLimit updates the per-IP requests-per-second and burst values.
	ActionAdjustRateLimit ActionType = "adjust_rate_limit"

	// ActionUpdateConfig applies a JSON merge-patch to the running configuration.
	ActionUpdateConfig ActionType = "update_config"
)

// SourceMCPAgent identifies the MCP agent as the source of a proposal.
const SourceMCPAgent = "mcp_agent"

// DefaultTTL is the TTL for a proposal that does not specify its own expiry.
const DefaultTTL = time.Hour

// Proposal is the central domain entity. It captures one pending config-change
// suggestion together with the context needed for a human to make a decision.
type Proposal struct {
	// ID is a UUID that uniquely identifies this proposal.
	ID string

	// Type is the kind of configuration change being proposed.
	Type ActionType

	// Params holds action-specific parameters (e.g. {"ip": "1.2.3.4"} for
	// block_ip, {"requests_per_second": 5, "burst": 10} for adjust_rate_limit).
	Params map[string]any

	// Reason is a human- and AI-readable explanation of why this change is
	// being suggested (e.g. "IP 1.2.3.4 made 500 requests in 60 seconds").
	Reason string

	// Diff is a preview of what the configuration would look like after the
	// change is applied, expressed as a unified diff or a human-readable
	// summary. Populated by the store at creation time.
	Diff string

	// Status is the current lifecycle state of the proposal.
	Status Status

	// CreatedAt is when the proposal was created (UTC).
	CreatedAt time.Time

	// ExpiresAt is when the proposal will auto-expire if not acted upon (UTC).
	ExpiresAt time.Time

	// Source identifies the component that created this proposal.
	// Expected value: "mcp_agent".
	Source string
}

// IsExpired reports whether the proposal has passed its expiry time.
func (p *Proposal) IsExpired(now time.Time) bool {
	return now.After(p.ExpiresAt)
}
