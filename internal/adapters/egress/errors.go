package egress

import "errors"

// ErrDeniedByPolicy is returned by Proxy.HandleRequest when the request does
// not match any configured route and the default policy is deny.
var ErrDeniedByPolicy = errors.New("egress: request denied by policy")

// ErrCircuitOpen is returned by Proxy.HandleRequest when the per-route circuit
// breaker is in the open state and the request is short-circuited before any
// upstream contact is made.
var ErrCircuitOpen = errors.New("egress: circuit breaker is open")

// ErrRateLimitExceeded is returned by Proxy.HandleRequest when the per-route
// rate limiter has run out of tokens and the request is rejected before any
// upstream contact is made.
var ErrRateLimitExceeded = errors.New("egress: rate limit exceeded")

// ErrRequestBodyTooLarge is returned by Proxy.HandleRequest when the incoming
// request body exceeds the configured body size limit. The HTTP handler converts
// this into a 413 Request Entity Too Large response.
var ErrRequestBodyTooLarge = errors.New("egress: request body exceeds size limit")

// ErrInsecureURL is returned by Proxy.HandleRequest when the target URL uses
// plain HTTP and neither the proxy-level AllowInsecure flag nor the matched
// route's AllowInsecure flag is set. The HTTP handler converts this into a
// 400 Bad Request response.
var ErrInsecureURL = errors.New("egress: plain HTTP is not allowed; use HTTPS or set allow_insecure")
