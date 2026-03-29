package egress_test

import (
	"context"
	"testing"

	egressadapter "github.com/vibewarden/vibewarden/internal/adapters/egress"
	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestRouteResolver_Resolve(t *testing.T) {
	stripe := newTestRoute(t, "stripe", "https://api.stripe.com/v1/*")
	github := newTestRoute(t, "github", "https://api.github.com/repos/*",
		domainegress.WithMethods("GET"),
	)

	tests := []struct {
		name        string
		routes      []domainegress.Route
		method      string
		url         string
		wantMatched bool
		wantRoute   string
	}{
		{
			name:        "matches stripe route",
			routes:      []domainegress.Route{stripe, github},
			method:      "POST",
			url:         "https://api.stripe.com/v1/charges",
			wantMatched: true,
			wantRoute:   "stripe",
		},
		{
			name:        "matches github route on GET",
			routes:      []domainegress.Route{stripe, github},
			method:      "GET",
			url:         "https://api.github.com/repos/myrepo",
			wantMatched: true,
			wantRoute:   "github",
		},
		{
			name:        "github route does not match POST",
			routes:      []domainegress.Route{stripe, github},
			method:      "POST",
			url:         "https://api.github.com/repos/myrepo",
			wantMatched: false,
		},
		{
			name:        "no routes returns unmatched",
			routes:      nil,
			method:      "GET",
			url:         "https://api.stripe.com/v1/charges",
			wantMatched: false,
		},
		{
			name:        "unknown URL returns unmatched",
			routes:      []domainegress.Route{stripe},
			method:      "GET",
			url:         "https://api.unknown.example.com/",
			wantMatched: false,
		},
		{
			name:        "first matching route wins",
			routes:      []domainegress.Route{stripe, stripe},
			method:      "GET",
			url:         "https://api.stripe.com/v1/charges",
			wantMatched: true,
			wantRoute:   "stripe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := egressadapter.NewRouteResolver(tt.routes)

			req, err := domainegress.NewEgressRequest(tt.method, tt.url, nil, nil)
			if err != nil {
				t.Fatalf("NewEgressRequest: %v", err)
			}

			match, err := resolver.Resolve(context.Background(), req)
			if err != nil {
				t.Fatalf("Resolve returned unexpected error: %v", err)
			}

			if match.Matched != tt.wantMatched {
				t.Errorf("Matched = %v, want %v", match.Matched, tt.wantMatched)
			}
			if tt.wantMatched && match.Route.Name() != tt.wantRoute {
				t.Errorf("Route.Name() = %q, want %q", match.Route.Name(), tt.wantRoute)
			}
		})
	}
}
