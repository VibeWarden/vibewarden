package ops

import (
	"context"
	"fmt"
	"net/http"
)

// HTTPHealthChecker implements ports.HealthChecker using net/http.
type HTTPHealthChecker struct {
	client *http.Client
}

// NewHTTPHealthChecker creates a new HTTPHealthChecker with the provided client.
// If client is nil, http.DefaultClient is used.
func NewHTTPHealthChecker(client *http.Client) *HTTPHealthChecker {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPHealthChecker{client: client}
}

// CheckHealth performs a GET request to url and returns true when the response
// status code is in the 2xx range.
func (h *HTTPHealthChecker) CheckHealth(ctx context.Context, url string) (bool, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, 0, fmt.Errorf("building request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return false, 0, fmt.Errorf("performing request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	return ok, resp.StatusCode, nil
}
