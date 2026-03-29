package egress

import (
	"errors"
	"net/http"
	"time"
)

// EgressResponse is a value object that carries the details of an outbound
// HTTP response received from an upstream service and forwarded back to the
// wrapped application. The body is referenced by an opaque handle rather than
// held in memory to avoid large allocations.
type EgressResponse struct {
	// StatusCode is the HTTP status code returned by the upstream service.
	StatusCode int

	// Header contains the HTTP response headers returned by the upstream service.
	// The egress proxy may strip or add headers before forwarding to the caller.
	Header http.Header

	// BodyRef is an opaque reference to the response body stream.
	// A nil value indicates an empty body.
	BodyRef interface{}

	// Duration is the wall-clock time elapsed from sending the request to
	// receiving the response headers from the upstream service.
	Duration time.Duration
}

// NewEgressResponse constructs an EgressResponse.
// Returns an error when statusCode is not a valid HTTP status (100–599).
func NewEgressResponse(statusCode int, header http.Header, bodyRef interface{}, duration time.Duration) (EgressResponse, error) {
	if statusCode < 100 || statusCode > 599 {
		return EgressResponse{}, errors.New("egress response status code must be in range 100-599")
	}
	if header == nil {
		header = make(http.Header)
	}
	return EgressResponse{
		StatusCode: statusCode,
		Header:     header,
		BodyRef:    bodyRef,
		Duration:   duration,
	}, nil
}
