package ports

import "net/http"

// HTTPClient is the outbound port used by the upgrade service to make HTTP
// requests. The narrow interface (Do only) keeps the upgrade use case testable
// without a real network connection.
//
// The upgrade_ prefix in the filename documents that this interface is scoped
// to the upgrade use case. A generic http_client.go filename would imply a
// shared HTTP port and invite catch-all misuse.
type HTTPClient interface {
	// Do executes an HTTP request and returns the response.
	Do(req *http.Request) (*http.Response, error)
}
