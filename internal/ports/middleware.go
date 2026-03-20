package ports

import "net/http"

// Middleware defines an HTTP middleware that can be applied to requests.
// Middleware wraps an http.Handler and returns a new http.Handler.
type Middleware func(next http.Handler) http.Handler

// MiddlewareChain is an ordered list of middleware to apply.
type MiddlewareChain []Middleware

// Apply wraps the given handler with all middleware in the chain.
// Middleware is applied in reverse order so the first middleware in the chain
// is the outermost wrapper (executes first on request, last on response).
func (c MiddlewareChain) Apply(h http.Handler) http.Handler {
	for i := len(c) - 1; i >= 0; i-- {
		h = c[i](h)
	}
	return h
}
