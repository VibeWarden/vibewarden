package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vibewarden/vibewarden/internal/middleware"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newGatedAdminServer starts a test server whose /_vibewarden/admin/* routes
// are protected by the REAL AdminAuthMiddleware, not a hand-rolled header
// check.
//
// The MCP tools authenticate with Authorization: Bearer. Before #1513 the
// middleware read only X-Admin-Key, so every MCP tool failed against a real
// sidecar while these tests stayed green against fakes that happened to accept
// Bearer. Routing the fakes through the production gate makes that class of
// drift impossible.
func newGatedAdminServer(t *testing.T, token string, h http.Handler) *httptest.Server {
	t.Helper()

	gate := middleware.AdminAuthMiddleware(
		ports.AdminAuthConfig{Enabled: true, Token: token},
		nil,
		nil,
	)
	srv := httptest.NewServer(gate(h))
	t.Cleanup(srv.Close)
	return srv
}
