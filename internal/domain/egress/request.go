package egress

import (
	"errors"
	"net/http"
)

// EgressRequest is a value object that carries the details of an outbound
// HTTP request intercepted by the egress proxy. The body is referenced by an
// opaque handle rather than held in memory to avoid large allocations.
type EgressRequest struct {
	// Method is the HTTP method of the outbound request (e.g. "GET", "POST").
	Method string

	// URL is the destination URL of the outbound request.
	URL string

	// Header contains the HTTP headers sent with the request.
	// The egress proxy may add, modify, or remove headers before forwarding.
	Header http.Header

	// BodyRef is an opaque reference to the request body stream.
	// A nil value indicates an empty body.
	BodyRef interface{}
}

// NewEgressRequest constructs an EgressRequest.
// Returns an error when method or URL is empty.
func NewEgressRequest(method, rawURL string, header http.Header, bodyRef interface{}) (EgressRequest, error) {
	if method == "" {
		return EgressRequest{}, errors.New("egress request method cannot be empty")
	}
	if rawURL == "" {
		return EgressRequest{}, errors.New("egress request URL cannot be empty")
	}
	if header == nil {
		header = make(http.Header)
	}
	return EgressRequest{
		Method:  method,
		URL:     rawURL,
		Header:  header,
		BodyRef: bodyRef,
	}, nil
}
