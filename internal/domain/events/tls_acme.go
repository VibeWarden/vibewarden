package events

import (
	"fmt"
	"strings"
	"time"
)

// TLSACMEChainSkippedParams contains the parameters needed to construct a
// tls.acme.chain_skipped event.
type TLSACMEChainSkippedParams struct {
	// Provider is the ACME issuer that was skipped (e.g. "zerossl").
	Provider string

	// Reason is a machine-readable skip reason (e.g. "email_not_configured").
	// The v1 schema freezes the set of allowed values; see ADR-083.
	Reason string

	// PrimaryProvider is the primary ACME provider whose default chain was
	// being built (e.g. "letsencrypt"). Included so log aggregators can
	// correlate the skip to the parent chain.
	PrimaryProvider string
}

// NewTLSACMEChainSkipped creates a tls.acme.chain_skipped event indicating
// that an ACME issuer was evaluated for the default fallback chain but
// excluded (e.g. ZeroSSL skipped because tls.email is empty).
func NewTLSACMEChainSkipped(params TLSACMEChainSkippedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeTLSACMEChainSkipped,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryNetwork,
		AISummary: fmt.Sprintf(
			"ACME issuer %s skipped in fallback chain for %s: %s — set tls.email in your config to enable it",
			params.Provider, params.PrimaryProvider, params.Reason,
		),
		Payload: map[string]any{
			"provider":         params.Provider,
			"reason":           params.Reason,
			"primary_provider": params.PrimaryProvider,
		},
	}
}

// TLSACMEChainFallbackParams contains the parameters needed to construct a
// tls.acme.chain_fallback event.
type TLSACMEChainFallbackParams struct {
	// FromProvider is the issuer that failed (e.g. "letsencrypt").
	FromProvider string

	// ToProvider is the issuer the chain fell back to (e.g. "zerossl").
	ToProvider string

	// Reason is a machine-readable classification of the transition (e.g.
	// "upstream_unreachable", "rate_limited", "unknown"). See ADR-083 for
	// the v1-frozen set.
	Reason string

	// Domain is the certificate subject the fallback was for.
	Domain string
}

// NewTLSACMEChainFallback creates a tls.acme.chain_fallback event indicating
// that Caddy transitioned between issuers in an active fallback chain.
//
// NOTE: Emission of this event requires a stable runtime hook from
// Caddy/certmagic/acmez. If no such hook is exposed, TLS plugin Init falls
// back to emitting tls.acme.chain_configured only. See ADR-083 §3b.
func NewTLSACMEChainFallback(params TLSACMEChainFallbackParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeTLSACMEChainFallback,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityMedium,
		Category:      CategoryNetwork,
		AISummary: fmt.Sprintf(
			"ACME issuer failover for %s: %s → %s (%s)",
			params.Domain, params.FromProvider, params.ToProvider, params.Reason,
		),
		Payload: map[string]any{
			"from_provider": params.FromProvider,
			"to_provider":   params.ToProvider,
			"reason":        params.Reason,
			"domain":        params.Domain,
		},
	}
}

// TLSACMEChainConfiguredParams contains the parameters needed to construct a
// tls.acme.chain_configured event.
type TLSACMEChainConfiguredParams struct {
	// PrimaryProvider is the configured primary ACME provider (e.g.
	// "letsencrypt", "zerossl", "buypass", "letsencrypt-staging").
	PrimaryProvider string

	// ResolvedChain is the ordered list of issuer identifiers Caddy will try
	// (e.g. ["letsencrypt", "zerossl"]).
	ResolvedChain []string

	// Domain is the certificate subject the chain will provision for.
	Domain string
}

// NewTLSACMEChainConfigured creates a tls.acme.chain_configured event
// capturing the resolved ACME issuer chain at plugin Init. Always emitted
// for ACME providers, regardless of whether any issuers were skipped.
func NewTLSACMEChainConfigured(params TLSACMEChainConfiguredParams) Event {
	// Defensive copy so downstream mutation cannot affect the event payload.
	chain := make([]string, len(params.ResolvedChain))
	copy(chain, params.ResolvedChain)

	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeTLSACMEChainConfigured,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryNetwork,
		AISummary: fmt.Sprintf(
			"ACME fallback chain configured for %s (primary=%s): %s",
			params.Domain, params.PrimaryProvider, strings.Join(chain, ","),
		),
		Payload: map[string]any{
			"primary_provider": params.PrimaryProvider,
			"resolved_chain":   chain,
			"domain":           params.Domain,
		},
	}
}

// TLSACMEProviderDeprecatedParams contains the parameters needed to construct
// a tls.acme.provider_deprecated event.
type TLSACMEProviderDeprecatedParams struct {
	// Provider is the deprecated provider identifier (e.g. "buypass").
	Provider string

	// Reason is a machine-readable reason the provider is deprecated (e.g.
	// "directory_returns_403").
	Reason string

	// Guidance is a short human-readable suggestion pointing the operator
	// at a supported alternative.
	Guidance string
}

// NewTLSACMEProviderDeprecated creates a tls.acme.provider_deprecated event
// warning that the operator's explicitly-selected ACME provider is
// currently unhealthy or otherwise discouraged. See ADR-083 §3d.
func NewTLSACMEProviderDeprecated(params TLSACMEProviderDeprecatedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeTLSACMEProviderDeprecated,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityMedium,
		Category:      CategoryNetwork,
		AISummary: fmt.Sprintf(
			"ACME provider %s is deprecated: %s — %s",
			params.Provider, params.Reason, params.Guidance,
		),
		Payload: map[string]any{
			"provider": params.Provider,
			"reason":   params.Reason,
			"guidance": params.Guidance,
		},
	}
}
