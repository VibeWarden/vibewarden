// Package health contains the domain model for upstream health checking.
// This package has zero external dependencies — only Go stdlib.
package health

import (
	"errors"
	"time"
)

// UpstreamStatus represents the health state of the upstream application.
type UpstreamStatus int

const (
	// StatusUnknown is the initial state before any probe has completed.
	StatusUnknown UpstreamStatus = iota

	// StatusHealthy means the upstream is responding with 2xx within the timeout.
	// This state requires at least healthy_threshold consecutive successes.
	StatusHealthy

	// StatusUnhealthy means the upstream has failed to respond with 2xx within
	// the timeout for at least unhealthy_threshold consecutive probes.
	StatusUnhealthy
)

// String returns a human-readable name for the status.
func (s UpstreamStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// Config holds the parameters that drive an UpstreamHealth checker.
type Config struct {
	// Enabled toggles the health checker.
	Enabled bool

	// Path is the HTTP path probed on the upstream (e.g. "/health").
	// Must be non-empty when Enabled is true.
	Path string

	// Interval is the time between consecutive probes.
	// Must be > 0 when Enabled is true.
	Interval time.Duration

	// Timeout is the maximum time to wait for a probe response.
	// Must be > 0 when Enabled is true.
	Timeout time.Duration

	// UnhealthyThreshold is the number of consecutive failures required to
	// transition from Healthy/Unknown to Unhealthy.
	// Must be > 0 when Enabled is true.
	UnhealthyThreshold int

	// HealthyThreshold is the number of consecutive successes required to
	// transition from Unhealthy/Unknown to Healthy.
	// Must be > 0 when Enabled is true.
	HealthyThreshold int
}

// Validate returns an error when the configuration is invalid.
func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Path == "" {
		return errors.New("upstream health path must not be empty")
	}
	if c.Interval <= 0 {
		return errors.New("upstream health interval must be greater than zero")
	}
	if c.Timeout <= 0 {
		return errors.New("upstream health timeout must be greater than zero")
	}
	if c.UnhealthyThreshold <= 0 {
		return errors.New("upstream health unhealthy_threshold must be greater than zero")
	}
	if c.HealthyThreshold <= 0 {
		return errors.New("upstream health healthy_threshold must be greater than zero")
	}
	return nil
}

// UpstreamHealth is a domain entity that tracks the health of an upstream
// application using threshold-based hysteresis.
//
// State transitions:
//   - After HealthyThreshold consecutive successes → Healthy
//   - After UnhealthyThreshold consecutive failures → Unhealthy
//   - Starts as Unknown
//
// UpstreamHealth is not safe for concurrent use on its own — callers must
// synchronise access externally.
type UpstreamHealth struct {
	cfg Config

	status     UpstreamStatus
	successes  int
	failures   int
	lastProbed time.Time
	lastError  string
}

// NewUpstreamHealth creates a new UpstreamHealth in the Unknown state with the
// given configuration. Returns an error when the configuration is invalid.
func NewUpstreamHealth(cfg Config) (*UpstreamHealth, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &UpstreamHealth{
		cfg:    cfg,
		status: StatusUnknown,
	}, nil
}

// Status returns the current upstream health status.
func (u *UpstreamHealth) Status() UpstreamStatus {
	return u.status
}

// Config returns the configuration used by this entity.
func (u *UpstreamHealth) Config() Config {
	return u.cfg
}

// LastProbed returns the time of the last completed probe. The zero value is
// returned when no probe has been completed.
func (u *UpstreamHealth) LastProbed() time.Time {
	return u.lastProbed
}

// LastError returns the error string from the most recent failed probe.
// Empty when the last probe succeeded.
func (u *UpstreamHealth) LastError() string {
	return u.lastError
}

// ConsecutiveSuccesses returns the current run of consecutive successes.
func (u *UpstreamHealth) ConsecutiveSuccesses() int {
	return u.successes
}

// ConsecutiveFailures returns the current run of consecutive failures.
func (u *UpstreamHealth) ConsecutiveFailures() int {
	return u.failures
}

// RecordSuccess records a successful probe at the given time.
// Returns the previous status and whether a state transition occurred.
func (u *UpstreamHealth) RecordSuccess(now time.Time) (previous UpstreamStatus, transitioned bool) {
	previous = u.status
	u.lastProbed = now
	u.lastError = ""
	u.failures = 0
	u.successes++

	if u.status != StatusHealthy && u.successes >= u.cfg.HealthyThreshold {
		u.status = StatusHealthy
		return previous, previous != StatusHealthy
	}
	return previous, false
}

// RecordFailure records a failed probe at the given time.
// Returns the previous status and whether a state transition occurred.
func (u *UpstreamHealth) RecordFailure(now time.Time, errMsg string) (previous UpstreamStatus, transitioned bool) {
	previous = u.status
	u.lastProbed = now
	u.lastError = errMsg
	u.successes = 0
	u.failures++

	if u.status != StatusUnhealthy && u.failures >= u.cfg.UnhealthyThreshold {
		u.status = StatusUnhealthy
		return previous, previous != StatusUnhealthy
	}
	return previous, false
}
