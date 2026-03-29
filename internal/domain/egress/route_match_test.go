package egress_test

import (
	"net/http"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestNewRouteMatch(t *testing.T) {
	req, err := egress.NewEgressRequest("GET", "https://api.stripe.com/v1/charges", nil, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest() error: %v", err)
	}
	route, err := egress.NewRoute("stripe", "https://api.stripe.com/v1/*")
	if err != nil {
		t.Fatalf("NewRoute() error: %v", err)
	}

	m := egress.NewRouteMatch(req, route)

	if !m.Matched {
		t.Error("Matched = false, want true")
	}
	if m.Route.Name() != route.Name() {
		t.Errorf("Route.Name() = %q, want %q", m.Route.Name(), route.Name())
	}
}

func TestNewUnmatchedRouteMatch(t *testing.T) {
	req, err := egress.NewEgressRequest("DELETE", "https://api.unknown.com/", http.Header{}, nil)
	if err != nil {
		t.Fatalf("NewEgressRequest() error: %v", err)
	}

	m := egress.NewUnmatchedRouteMatch(req)

	if m.Matched {
		t.Error("Matched = true, want false")
	}
	if m.Request.URL != req.URL {
		t.Errorf("Request.URL = %q, want %q", m.Request.URL, req.URL)
	}
}
