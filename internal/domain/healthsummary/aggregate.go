// Package healthsummary provides the domain-pure worst-component aggregator
// for the /_vibewarden/health outer status field.
//
// This package has zero external dependencies — only Go stdlib.
package healthsummary

// ComponentState is the minimal contract required by the aggregator.
// Both upstream.State and tls.State satisfy it; future components will too.
type ComponentState interface {
	// Healthy reports whether this component should NOT degrade the outer status.
	Healthy() bool
	// String returns the lowercase token used in the JSON components map.
	String() string
}

// Status is the outer wire status returned in the "status" field of
// /_vibewarden/health responses.
type Status string

const (
	// StatusOK means all components are healthy (or no components are registered).
	StatusOK Status = "ok"

	// StatusDegraded means one or more components are not healthy. The sidecar
	// is still serving traffic; this is informational, not a hard failure.
	StatusDegraded Status = "degraded"
)

// AggregateStatus returns the outer status given a map of named component
// states. The rule is worst-component-wins: if any component is not Healthy(),
// or if any map value is nil, the result is StatusDegraded. An empty map
// returns StatusOK. The evaluation order is not guaranteed.
func AggregateStatus(components map[string]ComponentState) Status {
	for _, c := range components {
		if c == nil || !c.Healthy() {
			return StatusDegraded
		}
	}
	return StatusOK
}
