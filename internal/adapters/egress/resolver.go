package egress

import (
	"context"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// RouteResolver implements ports.RouteResolver using an in-memory ordered list
// of routes. Routes are evaluated in declaration order; the first route whose
// URL pattern and method constraints both match wins.
type RouteResolver struct {
	routes []domainegress.Route
}

// NewRouteResolver constructs a RouteResolver from the given route list.
// Routes are evaluated in the order provided.
func NewRouteResolver(routes []domainegress.Route) *RouteResolver {
	return &RouteResolver{routes: routes}
}

// Resolve implements ports.RouteResolver. It iterates the configured routes in
// order and returns the first RouteMatch where both the URL pattern and HTTP
// method constraints are satisfied. When no route matches, it returns an
// unmatched RouteMatch with Matched == false.
func (r *RouteResolver) Resolve(_ context.Context, req domainegress.EgressRequest) (domainegress.RouteMatch, error) {
	for _, route := range r.routes {
		if route.MatchesURL(req.URL) && route.MatchesMethod(req.Method) {
			return domainegress.NewRouteMatch(req, route), nil
		}
	}
	return domainegress.NewUnmatchedRouteMatch(req), nil
}

// Interface guard — RouteResolver must implement ports.RouteResolver.
var _ ports.RouteResolver = (*RouteResolver)(nil)
