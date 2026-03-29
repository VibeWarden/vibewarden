package egress

import "errors"

// ErrDeniedByPolicy is returned by Proxy.HandleRequest when the request does
// not match any configured route and the default policy is deny.
var ErrDeniedByPolicy = errors.New("egress: request denied by policy")
