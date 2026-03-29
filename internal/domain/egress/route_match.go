package egress

// RouteMatch is a value object that pairs an EgressRequest with the Route
// that was resolved for it. It is produced by the RouteResolver port and
// consumed by the EgressProxy port to apply per-route settings.
type RouteMatch struct {
	// Request is the outbound request being evaluated.
	Request EgressRequest

	// Route is the matched route configuration.
	// Callers should check Matched before accessing Route.
	Route Route

	// Matched is true when a route was found for the request.
	// When false, the egress proxy applies the default policy.
	Matched bool
}

// NewRouteMatch constructs a RouteMatch indicating that a route was found.
func NewRouteMatch(req EgressRequest, route Route) RouteMatch {
	return RouteMatch{
		Request: req,
		Route:   route,
		Matched: true,
	}
}

// NewUnmatchedRouteMatch constructs a RouteMatch indicating that no route
// matched the request. The egress proxy applies the default policy.
func NewUnmatchedRouteMatch(req EgressRequest) RouteMatch {
	return RouteMatch{
		Request: req,
		Matched: false,
	}
}
