package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

// TestEgressRequestEnrichment verifies that NewEgressRequest populates the
// Actor, Resource, TriggeredBy, and TraceID fields correctly.
func TestEgressRequestEnrichment(t *testing.T) {
	tests := []struct {
		name        string
		params      events.EgressRequestParams
		wantRoute   string
		wantMethod  string
		wantTraceID string
	}{
		{
			name: "named route with trace",
			params: events.EgressRequestParams{
				Route:   "payments",
				Method:  "POST",
				URL:     "https://api.stripe.com/v1/charges",
				TraceID: "trace-abc",
			},
			wantRoute:   "payments",
			wantMethod:  "POST",
			wantTraceID: "trace-abc",
		},
		{
			name: "no trace id",
			params: events.EgressRequestParams{
				Route:  "analytics",
				Method: "GET",
				URL:    "https://analytics.example.com/data",
			},
			wantRoute:  "analytics",
			wantMethod: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := events.NewEgressRequest(tt.params)

			if e.Actor.Type != events.ActorTypeSystem {
				t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
			}
			if e.Resource.Type != events.ResourceTypeEgressRoute {
				t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
			}
			if e.Resource.Path != tt.wantRoute {
				t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, tt.wantRoute)
			}
			if e.Resource.Method != tt.wantMethod {
				t.Errorf("Resource.Method = %q, want %q", e.Resource.Method, tt.wantMethod)
			}
			if e.TraceID != tt.wantTraceID {
				t.Errorf("TraceID = %q, want %q", e.TraceID, tt.wantTraceID)
			}
			if e.TriggeredBy != "egress_proxy" {
				t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_proxy")
			}
		})
	}
}

// TestEgressResponseEnrichment verifies that NewEgressResponse populates the
// Actor, Resource, TriggeredBy, and TraceID fields correctly.
func TestEgressResponseEnrichment(t *testing.T) {
	params := events.EgressResponseParams{
		Route:           "payments",
		Method:          "POST",
		URL:             "https://api.stripe.com/v1/charges",
		StatusCode:      200,
		DurationSeconds: 0.123,
		Attempts:        1,
		TraceID:         "trace-def",
	}

	e := events.NewEgressResponse(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.Resource.Method != params.Method {
		t.Errorf("Resource.Method = %q, want %q", e.Resource.Method, params.Method)
	}
	if e.TraceID != params.TraceID {
		t.Errorf("TraceID = %q, want %q", e.TraceID, params.TraceID)
	}
	if e.TriggeredBy != "egress_proxy" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_proxy")
	}
}

// TestEgressBlockedEnrichment verifies that NewEgressBlocked populates the
// Actor, Resource, Outcome, TriggeredBy, and TraceID fields correctly.
func TestEgressBlockedEnrichment(t *testing.T) {
	params := events.EgressBlockedParams{
		Route:   "internal",
		Method:  "GET",
		URL:     "http://internal.corp/secrets",
		Reason:  "no route matched default deny policy",
		TraceID: "trace-ghi",
	}

	e := events.NewEgressBlocked(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.Outcome != events.OutcomeBlocked {
		t.Errorf("Outcome = %q, want %q", e.Outcome, events.OutcomeBlocked)
	}
	if e.TraceID != params.TraceID {
		t.Errorf("TraceID = %q, want %q", e.TraceID, params.TraceID)
	}
	if e.TriggeredBy != "egress_proxy" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_proxy")
	}
}

// TestEgressErrorEnrichment verifies that NewEgressError populates the
// Actor, Resource, Outcome, TriggeredBy, and TraceID fields correctly.
func TestEgressErrorEnrichment(t *testing.T) {
	params := events.EgressErrorParams{
		Route:    "payments",
		Method:   "POST",
		URL:      "https://api.stripe.com/v1/charges",
		Error:    "connection refused",
		Attempts: 3,
		TraceID:  "trace-jkl",
	}

	e := events.NewEgressError(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.Outcome != events.OutcomeFailed {
		t.Errorf("Outcome = %q, want %q", e.Outcome, events.OutcomeFailed)
	}
	if e.TraceID != params.TraceID {
		t.Errorf("TraceID = %q, want %q", e.TraceID, params.TraceID)
	}
	if e.TriggeredBy != "egress_proxy" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_proxy")
	}
}

// TestEgressResponseInvalidEnrichment verifies that NewEgressResponseInvalid
// populates the Actor, Resource, Outcome, TriggeredBy, and TraceID fields.
func TestEgressResponseInvalidEnrichment(t *testing.T) {
	params := events.EgressResponseInvalidParams{
		Route:       "api",
		Method:      "GET",
		URL:         "https://api.example.com/data",
		StatusCode:  500,
		ContentType: "text/html",
		Reason:      "status code not allowed",
		TraceID:     "trace-mno",
	}

	e := events.NewEgressResponseInvalid(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.Resource.Method != params.Method {
		t.Errorf("Resource.Method = %q, want %q", e.Resource.Method, params.Method)
	}
	if e.Outcome != events.OutcomeFailed {
		t.Errorf("Outcome = %q, want %q", e.Outcome, events.OutcomeFailed)
	}
	if e.TraceID != params.TraceID {
		t.Errorf("TraceID = %q, want %q", e.TraceID, params.TraceID)
	}
	if e.TriggeredBy != "egress_proxy" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_proxy")
	}
}

// TestEgressRateLimitHitEnrichment verifies that NewEgressRateLimitHit
// populates the Actor, Resource, Outcome, RiskSignals, and TriggeredBy fields.
func TestEgressRateLimitHitEnrichment(t *testing.T) {
	params := events.EgressRateLimitHitParams{
		Route:             "payments",
		Limit:             10.0,
		RetryAfterSeconds: 5.0,
	}

	e := events.NewEgressRateLimitHit(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.Outcome != events.OutcomeRateLimited {
		t.Errorf("Outcome = %q, want %q", e.Outcome, events.OutcomeRateLimited)
	}
	if len(e.RiskSignals) == 0 {
		t.Error("RiskSignals must not be empty for a rate limit hit event")
	} else if e.RiskSignals[0].Signal != "rate_limit_exceeded" {
		t.Errorf("RiskSignals[0].Signal = %q, want %q", e.RiskSignals[0].Signal, "rate_limit_exceeded")
	}
	if e.TriggeredBy != "egress_rate_limit_middleware" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_rate_limit_middleware")
	}
}

// TestEgressCircuitBreakerOpenedEnrichment verifies that
// NewEgressCircuitBreakerOpened populates the Actor, Resource, and TriggeredBy
// fields correctly.
func TestEgressCircuitBreakerOpenedEnrichment(t *testing.T) {
	params := events.EgressCircuitBreakerOpenedParams{
		Route:          "payments",
		Threshold:      5,
		TimeoutSeconds: 30.0,
	}

	e := events.NewEgressCircuitBreakerOpened(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.TriggeredBy != "egress_circuit_breaker" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_circuit_breaker")
	}
}

// TestEgressCircuitBreakerClosedEnrichment verifies that
// NewEgressCircuitBreakerClosed populates the Actor, Resource, and TriggeredBy
// fields correctly.
func TestEgressCircuitBreakerClosedEnrichment(t *testing.T) {
	params := events.EgressCircuitBreakerClosedParams{
		Route: "payments",
	}

	e := events.NewEgressCircuitBreakerClosed(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.TriggeredBy != "egress_circuit_breaker" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_circuit_breaker")
	}
}

// TestEgressSanitizedEnrichment verifies that NewEgressSanitized populates the
// Actor, Resource, TriggeredBy, and TraceID fields correctly.
func TestEgressSanitizedEnrichment(t *testing.T) {
	params := events.EgressSanitizedParams{
		Route:               "analytics",
		Method:              "POST",
		URL:                 "https://analytics.example.com/track",
		RedactedHeaders:     1,
		StrippedQueryParams: 2,
		RedactedBodyFields:  3,
		TraceID:             "trace-pqr",
	}

	e := events.NewEgressSanitized(params)

	if e.Actor.Type != events.ActorTypeSystem {
		t.Errorf("Actor.Type = %q, want %q", e.Actor.Type, events.ActorTypeSystem)
	}
	if e.Resource.Type != events.ResourceTypeEgressRoute {
		t.Errorf("Resource.Type = %q, want %q", e.Resource.Type, events.ResourceTypeEgressRoute)
	}
	if e.Resource.Path != params.Route {
		t.Errorf("Resource.Path = %q, want %q", e.Resource.Path, params.Route)
	}
	if e.Resource.Method != params.Method {
		t.Errorf("Resource.Method = %q, want %q", e.Resource.Method, params.Method)
	}
	if e.TraceID != params.TraceID {
		t.Errorf("TraceID = %q, want %q", e.TraceID, params.TraceID)
	}
	if e.TriggeredBy != "egress_sanitizer" {
		t.Errorf("TriggeredBy = %q, want %q", e.TriggeredBy, "egress_sanitizer")
	}
}
